# Paul's Problem

The Go service extracts weekly class drafts from timetable documents for the Android attendance tracker. The app can call the upload endpoint when a student imports a document. Semester setup, attendance, calendars, and backups belong to the Android app.

## Run the extraction service

Install Go 1.25.3 or later, MuPDF tools (`mutool`), and Tesseract with English language data.

- macOS with Homebrew — `brew install mupdf tesseract`
- Debian or Ubuntu — `sudo apt install mupdf-tools tesseract-ocr tesseract-ocr-eng`

Check that `mutool` and `tesseract` are on your PATH. Run:

```sh
make run
curl --fail http://localhost:8080/healthz
curl --fail-with-body \
  --form 'file=@internal/timetable/testdata/grid.pdf' \
  http://localhost:8080/v1/timetables/extract
```

To build a binary, run `make build`. Start it with `./bin/attendance-extractor`. Set `LISTEN_ADDR` to change the default address, `:8080`. For private remote use, restrict access at the network or reverse proxy and use HTTPS. The service has no built-in authentication.

## API and extraction

The [OpenAPI contract](docs/openapi.yaml) describes the health and upload endpoints. The [acceptance test plan](docs/api-test-plan.md) describes the expected input, output, and failure behavior.

Upload one PDF, PNG, JPEG, XLS, XLSX, or UTF-8 CSV of up to 10 MiB. Recognition covers weekday-by-time grids, tabular records, and text under weekday headings. It uses Tesseract for images and scanned PDF pages. OCR quality depends on the source. Review entries and warnings before saving the timetable.

Each upload uses request-scoped temporary storage. The service deletes that workspace when the request ends. The API returns a draft and stores no attendance or user records.

## Tests

```sh
make test
REQUIRE_DOCUMENT_TOOLS=1 go test -race ./...
```

Tests call the extractor directly with real files and exercise the Gin handler in-process with `httptest`. Set `REQUIRE_DOCUMENT_TOOLS=1` to require OCR and PDF fixture tests on the test machine.
