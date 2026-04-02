# Session recording and timelapse generation

A user wants to record a terminal session and generate a compressed timelapse.
Sessions are automatically recorded in asciicast v2 format when timelapse is
enabled (the default). The user can then compress the recording into a
timelapse that strips idle time, and download both the original and compressed
versions.

## Preconditions

- The daemon is running with timelapse recording enabled (default config)
- A session is spawned that produces terminal output
  (e.g., an agent running `sh -c 'for i in $(seq 1 20); do echo "line $i"; sleep 0.1; done; sleep 1; echo done'`)
- The session has produced enough output for a recording to exist

## Verifications

- GET /api/timelapse returns at least one recording
- The recording's SessionID matches the spawned session's ID
- The recording has a non-zero FileSize
- The recording has Duration > 0
- GET /api/timelapse/{recordingId}/download returns 200 with the original .cast file
- The downloaded .cast file starts with a valid asciicast v2 JSON header (version: 2)
- POST /api/timelapse/{recordingId}/export returns 200 (creates compressed timelapse)
- GET /api/timelapse/{recordingId}/download?type=timelapse returns 200 with the compressed file
- The compressed file is smaller than or equal to the original file
- The timelapse page at /timelapse shows the recording in the table
- The session detail page shows a "Make timelapse" button that is not disabled
- DELETE /api/timelapse/{recordingId} returns 204
- GET /api/timelapse no longer includes the deleted recording
- When timelapse is disabled in config, the Timelapse sidebar link is hidden
- When timelapse is disabled, the "Make timelapse" button on the session detail page is hidden
