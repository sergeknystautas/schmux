# Attach files when spawning

A user starting a fresh spawn attaches a CSV with the picker and a JPEG by
dropping it on the form, reloads the page, and submits. The spawn request
references the uploaded CSV and carries the JPEG with its real media type.

## Preconditions

- The isolated scenario daemon serves the real dashboard with one local git repository configured.
- The scenario image has no promptable agent, so `POST /api/spawn` is a controlled fixture that records the request and returns one successful session. `POST /api/spawn-attachments` is the real daemon endpoint.

## Verifications

- Attach opens a picker; choosing `users.csv` shows a `users.csv` chip.
- The upload reaches `POST /api/spawn-attachments` with the original filename and exact bytes, and the daemon answers 201 with an id.
- Dragging files over the spawn form shows the "Drop files to attach" outline; dropping `photo.jpg` shows an image thumbnail chip.
- Reloading the page restores both chips without uploading the CSV again.
- Submitting sends `file_attachments` equal to the id the daemon returned and `images` with one entry whose `media_type` is `image/jpeg`.
- After the successful spawn, both chips are gone.
