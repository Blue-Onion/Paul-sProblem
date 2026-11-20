package api

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Blue-Onion/Paul-sProblem/internal/timetable"
	"github.com/gin-gonic/gin"
)

const (
	maxFile    = 10 << 20
	maxRequest = 11 << 20
)

type Extractor func(context.Context, string, timetable.Format) (timetable.Result, error)

type Options struct {
	Timeout  time.Duration
	Parallel int
	TempDir  string
}

func New(extract Extractor, options Options) http.Handler {
	if options.Timeout <= 0 {
		options.Timeout = 30 * time.Second
	}
	if options.Parallel <= 0 {
		options.Parallel = 4
	}
	slots := make(chan struct{}, options.Parallel)
	router := gin.New()
	router.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	router.POST("/v1/timetables/extract", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequest)
		dir, path, format, err := stage(c.Request, options.TempDir)
		if dir != "" {
			defer os.RemoveAll(dir)
		}
		if err != nil {
			respond(c, err)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			respond(c, errBusy)
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), options.Timeout)
		defer cancel()
		result, err := extract(ctx, path, format)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		if err != nil {
			respond(c, err)
			return
		}
		if result.Entries == nil {
			result.Entries = []timetable.Entry{}
		}
		if result.Warnings == nil {
			result.Warnings = []timetable.Warning{}
		}
		c.JSON(http.StatusOK, result)
	})
	return router
}

var (
	errInvalid = errors.New("expected exactly one nonempty file part")
	errLarge   = errors.New("upload exceeds the size limit")
	errMedia   = errors.New("unsupported or mismatched document format")
	errBusy    = errors.New("all extraction slots are occupied")
)

func stage(r *http.Request, root string) (dir, path string, format timetable.Format, err error) {
	if r.ContentLength > maxRequest {
		return "", "", "", errLarge
	}
	media, _, parseErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "multipart/form-data" {
		return "", "", "", errMedia
	}
	if parseErr != nil {
		return "", "", "", errInvalid
	}
	reader, err := r.MultipartReader()
	if err != nil {
		return "", "", "", errInvalid
	}
	part, err := reader.NextPart()
	if err != nil {
		return "", "", "", multipartError(err)
	}
	if part.FormName() != "file" || part.FileName() == "" {
		return "", "", "", errInvalid
	}
	format = timetable.FromFilename(part.FileName())
	if format == "" {
		return "", "", "", errMedia
	}
	dir, err = os.MkdirTemp(root, "attendance-")
	if err != nil {
		return "", "", "", err
	}
	path = filepath.Join(dir, "input."+string(format))
	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return dir, "", "", err
	}
	n, copyErr := io.Copy(dst, io.LimitReader(part, maxFile+1))
	closeErr := dst.Close()
	switch {
	case n > maxFile:
		return dir, "", "", errLarge
	case copyErr != nil:
		return dir, "", "", multipartError(copyErr)
	case closeErr != nil:
		return dir, "", "", closeErr
	case n == 0:
		return dir, "", "", errInvalid
	}
	if _, err := reader.NextPart(); err != io.EOF {
		return dir, "", "", multipartError(err)
	}
	// Multipart permits an epilogue after the final boundary. Count it against the request limit.
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		return dir, "", "", multipartError(err)
	}
	if err := timetable.Validate(path, format); err != nil {
		if errors.Is(err, timetable.ErrFormat) {
			err = errMedia
		}
		return dir, "", "", err
	}
	return dir, path, format, nil
}

func multipartError(err error) error {
	var tooLarge *http.MaxBytesError
	var fileError *os.PathError
	switch {
	case errors.As(err, &tooLarge):
		return errLarge
	case errors.As(err, &fileError):
		return err
	default:
		return errInvalid
	}
}

func respond(c *gin.Context, err error) {
	status, code, message := 500, "INTERNAL_ERROR", "Unexpected extraction failure"
	switch {
	case errors.Is(err, errInvalid):
		status, code, message = 400, "INVALID_UPLOAD", errInvalid.Error()
	case errors.Is(err, errLarge):
		status, code, message = 413, "UPLOAD_TOO_LARGE", errLarge.Error()
	case errors.Is(err, errMedia):
		status, code, message = 415, "UNSUPPORTED_MEDIA_TYPE", errMedia.Error()
	case errors.Is(err, timetable.ErrDocument):
		status, code, message = 422, "UNPROCESSABLE_DOCUMENT", "The document is damaged, encrypted, or exceeds a processing limit"
	case errors.Is(err, timetable.ErrProcessor):
		status, code, message = 503, "PROCESSOR_UNAVAILABLE", "A required document processor is unavailable"
	case errors.Is(err, errBusy):
		status, code, message = 503, "SERVICE_BUSY", errBusy.Error()
	case errors.Is(err, context.DeadlineExceeded):
		status, code, message = 504, "EXTRACTION_TIMEOUT", "Extraction exceeded its deadline"
	}
	c.JSON(status, gin.H{"code": code, "message": message})
}
