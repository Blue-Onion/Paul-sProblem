# Timetable extraction API review

The Go service exposes two public endpoints. `GET /healthz` checks HTTP liveness. `POST /v1/timetables/extract` returns an editable weekly draft. Android owns all user records and applies semester dates, working-day rules, and holidays after review. Timetable extraction needs a connection to the service. Manual entry remains available offline.

## Extraction behavior

Accept exactly one multipart `file` part. Check the filename extension and inspect the file's content independently of the part's declared MIME type. Accept case-insensitive extensions. Treat XML or ZIP containers as spreadsheets only after identifying their actual format. Detect encrypted or corrupt documents after recognizing their format.

Use embedded PDF text and positions where available, with OCR fallback on scanned pages. Preserve coordinates for image OCR and sheet rows and columns for spreadsheets. Recognize a conventional grid with weekdays on one axis and time slots on the other, and text rows or weekday headings with explicit time ranges. Normalize 12-hour and 24-hour input to local `HH:mm`. Use explicit markers such as “lab” or “practical” for practical sessions. Use `unknown` when the class type cannot be inferred and omit the room when unavailable. Skip candidates with ambiguous required fields and emit `AMBIGUOUS_ENTRY`. Never guess a missing end time. Return `NO_ENTRIES` if nothing usable remains.

Trim and collapse whitespace in subjects and rooms. Deduplicate entries matching weekday, start time, end time, subject, kind and room after normalization, with case-insensitive subject and room comparison. Keep the first spelling and emit `DUPLICATE_ENTRY`. Sort by weekday, start time, end time, subject, room and kind. Separate lectures on the same day remain separate.

Apply a 30-second processing deadline and a four-extraction concurrency limit per process. Limit input to 40 PDF pages, 25 megapixels per image or rendered page, and 100,000 populated spreadsheet cells per workbook. Cap decompressed XLSX data at 64 MiB. Use request-scoped temporary storage and remove it on success, error, timeout and client cancellation. Keep document content, filenames and raw processor output out of logs and error messages.

## Endpoint acceptance tests

Use Go HTTP tests with an injected extractor for protocol and failure behavior, plus integration tests with real document fixtures and processors. Assert JSON content types, status codes, error codes, required fields and array shapes. Check fixtures into the repo when implementation begins.

### Health

- **H01** · `GET /healthz` returns 200 and exactly `{"status":"ok"}`.
- **H02** · A simulated extractor outage leaves liveness at 200.

### Upload validation and failures

- **U01** · A valid single-file request calls extraction once and returns the draft.
- **U02** · Missing, empty, duplicate or unexpected multipart parts and malformed multipart framing return 400 with `INVALID_UPLOAD`.
- **U03** · Exactly 10 MiB of otherwise valid file data reaches extraction. One byte more returns 413 with `UPLOAD_TOO_LARGE`.
- **U04** · Request bodies over 11 MiB return 413 with `UPLOAD_TOO_LARGE`, including when sent without Content-Length.
- **U05** · Non-multipart content, unsupported extensions, and recognizable content with a mismatched extension return 415 with `UNSUPPORTED_MEDIA_TYPE`.
- **U06** · An uppercase supported extension is accepted. A generic part MIME type works when the document content and extension agree.
- **U07** · Corrupt and encrypted files with a recognized format and documents above each complexity limit return 422 with `UNPROCESSABLE_DOCUMENT`.
- **U08** · An unexpected extractor error returns 500 with `INTERNAL_ERROR`, with no server paths or processor details.
- **U09** · A missing required processor returns 503 with `PROCESSOR_UNAVAILABLE`. An ordinary CSV still works when OCR is unavailable.
- **U10** · Four blocked extractions occupy the configured slots. The next valid request returns 503 with `SERVICE_BUSY`. A new request succeeds after a slot is released.
- **U11** · An extractor exceeding the deadline is cancelled, returns 504 with `EXTRACTION_TIMEOUT`, and releases its slot. Use a shorter test deadline.
- **U12** · Client cancellation stops processing and releases the slot. Temporary artifacts are removed after success, processing failure, timeout, and client cancellation. A hostile-looking filename is confined to the request workspace and absent from logs.

### Format and recognition fixtures

- **F01** · A text PDF grid with weekday rows and time columns yields the expected subjects, rooms and lecture times.
- **F02** · A scanned PDF and PNG/JPEG grids with weekday columns and time rows recover the expected classes via Tesseract. Include a PDF with both embedded text and a scanned page.
- **F03** · XLSX, legacy XLS and UTF-8 CSV recover the expected classes from equivalent schedules. Include a multi-sheet workbook, a quoted CSV subject with a comma and a UTF-8 BOM.
- **F04** · Free text under weekday headings parses explicit time ranges, including noon and afternoon. Practical labels and rooms survive normalization, and an unlabelled session has kind `unknown`.
- **F05** · ISO weekdays include Saturday and Sunday. Two sessions of the same subject at different times on one day appear in time order.
- **F06** · Repeated matching rows yield one entry and `DUPLICATE_ENTRY`. Distinct overlapping candidates remain available for review.
- **F07** · A document with one valid class, one class missing its end time and one class with a reversed time range returns the valid class and ambiguity warnings.
- **F08** · A readable document with no classes returns 200, an empty `entries` array and `NO_ENTRIES`. A complete extraction returns an empty `warnings` array.
- **F09** · Validate real response payloads against the OpenAPI schema, including the optional room and warning location, ISO weekdays, and local time format.

Calendar generation, holiday and Saturday rules, attendance denominators and projections belong to the Android test suite.
