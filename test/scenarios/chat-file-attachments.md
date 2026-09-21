# Attach a file to a chat message

A user attaches a non-image file from their browser to an existing chat, reloads
the page, and sends its saved workspace path to the agent. Images continue to
travel inline. The composer displays both attachment types in light and dark mode.

## Preconditions

- The dashboard is running with a connected chat session in a local workspace.
- Chat WebSocket records and upload responses are controlled fixtures, so no model service is needed.

## Verifications

- Attach opens a picker that accepts a non-image file and an image together.
- The file upload request carries the original filename and exact raw bytes to the current workspace's attachment endpoint.
- The composer shows the non-image filename and image thumbnail.
- Reloading restores the draft's file reference and image without uploading the file again.
- Sending includes the saved file path in the text and the image in the image payload.
- Sending clears both attachment chips.
