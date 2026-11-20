package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Blue-Onion/Paul-sProblem/internal/timetable"
)

const csvFixture = "Day,Time,Subject\nMonday,9:00-10:00,Physics\n"

func upload(t *testing.T, filename, data string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/timetables/extract", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestEndpoints(t *testing.T) {
	for _, tt := range []struct {
		name, filename, data, code string
		status                     int
		failure                    error
	}{
		{"success", "TIMETABLE.CSV", csvFixture, "", 200, nil},
		{"empty", "schedule.csv", "", "INVALID_UPLOAD", 400, nil},
		{"extension", "schedule.doc", csvFixture, "UNSUPPORTED_MEDIA_TYPE", 415, nil},
		{"mismatch", "schedule.png", csvFixture, "UNSUPPORTED_MEDIA_TYPE", 415, nil},
		{"corrupt", "schedule.pdf", "%PDF-1.7", "UNPROCESSABLE_DOCUMENT", 422, timetable.ErrDocument},
		{"processor", "schedule.csv", csvFixture, "PROCESSOR_UNAVAILABLE", 503, timetable.ErrProcessor},
		{"internal", "schedule.csv", csvFixture, "INTERNAL_ERROR", 500, errors.New("/private/input")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var staged string
			server := New(func(_ context.Context, path string, _ timetable.Format) (timetable.Result, error) {
				staged = path
				if _, err := os.Stat(path); err != nil {
					t.Error(err)
				}
				return timetable.Result{}, tt.failure
			}, Options{})
			health := httptest.NewRecorder()
			server.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if health.Code != 200 || health.Body.String() != `{"status":"ok"}` {
				t.Fatal(health.Body.String())
			}
			response := httptest.NewRecorder()
			server.ServeHTTP(response, upload(t, tt.filename, tt.data))
			assertResponse(t, response, tt.status, tt.code)
			if staged != "" {
				if _, err := os.Stat(staged); !os.IsNotExist(err) {
					t.Fatalf("staged upload remains: %v", err)
				}
			}
		})
	}
}

func assertResponse(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("got %d %s", response.Code, response.Body.String())
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if code != "" && string(value["code"]) != "\""+code+"\"" {
		t.Fatal(response.Body.String())
	}
	if code == "" && (string(value["entries"]) != "[]" || string(value["warnings"]) != "[]") {
		t.Fatal(response.Body.String())
	}
	if strings.Contains(response.Body.String(), "/private") {
		t.Fatal("disclosed processor details")
	}
}

func TestDeadline(t *testing.T) {
	server := New(func(ctx context.Context, _ string, _ timetable.Format) (timetable.Result, error) {
		<-ctx.Done()
		return timetable.Result{}, ctx.Err()
	}, Options{Timeout: 10 * time.Millisecond, Parallel: 1})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, upload(t, "schedule.csv", csvFixture))
	assertResponse(t, response, 504, "EXTRACTION_TIMEOUT")
}

func TestMultipartProtocol(t *testing.T) {
	server := New(func(context.Context, string, timetable.Format) (timetable.Result, error) {
		t.Error("invalid request reached extractor")
		return timetable.Result{}, nil
	}, Options{})
	for _, tc := range []struct {
		name, contentType, content, code string
		status                           int
	}{
		{"non-multipart", "text/plain", csvFixture, "UNSUPPORTED_MEDIA_TYPE", 415},
		{"no boundary", "multipart/form-data", csvFixture, "INVALID_UPLOAD", 400},
		{"no file", "multipart/form-data; boundary=test", "--test--\r\n", "INVALID_UPLOAD", 400},
		{"unexpected field", "multipart/form-data; boundary=test", "--test\r\nContent-Disposition: form-data; name=\"extra\"\r\n\r\nx\r\n--test--\r\n", "INVALID_UPLOAD", 400},
		{"truncated", "multipart/form-data; boundary=test", "--test\r\nContent-Disposition: form-data; name=\"file\"; filename=\"class.csv\"\r\n\r\nPhysics", "INVALID_UPLOAD", 400},
		{"duplicate", "multipart/form-data; boundary=test", strings.Repeat("--test\r\nContent-Disposition: form-data; name=\"file\"; filename=\"class.csv\"\r\n\r\nPhysics\r\n", 2) + "--test--\r\n", "INVALID_UPLOAD", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/timetables/extract", strings.NewReader(tc.content))
			req.Header.Set("Content-Type", tc.contentType)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, req)
			assertResponse(t, response, tc.status, tc.code)
		})
	}
}

func TestUploadLimits(t *testing.T) {
	server := New(func(_ context.Context, path string, _ timetable.Format) (timetable.Result, error) {
		file, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if file.Size() != maxFile {
			t.Errorf("staged size %d", file.Size())
		}
		return timetable.Result{}, nil
	}, Options{})
	for _, tc := range []struct {
		size, epilogue, want int
		chunked              bool
	}{
		{maxFile, 0, 200, false},
		{maxFile + 1, 0, 413, false},
		{maxFile, 2 << 20, 413, false},
		{maxFile, 2 << 20, 413, true},
	} {
		req := upload(t, "timetable.csv", strings.Repeat("a", tc.size))
		if tc.epilogue > 0 {
			req.Body = io.NopCloser(io.MultiReader(req.Body, strings.NewReader(strings.Repeat("a", tc.epilogue))))
			req.ContentLength += int64(tc.epilogue)
		}
		if tc.chunked {
			req.ContentLength = -1
			req.TransferEncoding = []string{"chunked"}
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		code := ""
		if tc.want == 413 {
			code = "UPLOAD_TOO_LARGE"
		}
		assertResponse(t, response, tc.want, code)
	}
}

func TestCapacityAndCancellation(t *testing.T) {
	entered := make(chan string, 4)
	var cancellations []context.CancelFunc
	var completions []chan struct{}
	temp := t.TempDir()
	server := New(func(ctx context.Context, path string, _ timetable.Format) (timetable.Result, error) {
		entered <- path
		<-ctx.Done()
		return timetable.Result{}, ctx.Err()
	}, Options{Timeout: 5 * time.Second, TempDir: temp})
	defer func() {
		for _, cancel := range cancellations {
			cancel()
		}
	}()
	for i := 0; i < 4; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancellations = append(cancellations, cancel)
		req := upload(t, "../../private.csv", csvFixture).WithContext(ctx)
		done := make(chan struct{})
		go func() { defer close(done); server.ServeHTTP(httptest.NewRecorder(), req) }()
		completions = append(completions, done)
		select {
		case path := <-entered:
			if !strings.HasPrefix(path, temp+string(os.PathSeparator)) || strings.Contains(path, "private.csv") {
				t.Fatalf("unexpected staged name %q", path)
			}
		case <-time.After(time.Second):
			t.Fatal("extraction did not start")
		}
	}
	req := upload(t, "overflow.csv", csvFixture)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)
	assertResponse(t, response, 503, "SERVICE_BUSY")
	for i, cancel := range cancellations {
		cancel()
		select {
		case <-completions[i]:
		case <-time.After(time.Second):
			t.Fatal("extractor did not return after cancellation")
		}
	}
	files, err := os.ReadDir(temp)
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary uploads remain: %v %v", files, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req = upload(t, "another.csv", csvFixture).WithContext(ctx)
	done := make(chan struct{})
	go func() { defer close(done); server.ServeHTTP(httptest.NewRecorder(), req) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("released capacity could not be reused")
	}
	cancel()
	<-done
}
