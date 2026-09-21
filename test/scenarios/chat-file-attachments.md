# Attach a file to a chat message

A user attaches a non-image file from their browser to an existing chat, reloads
the page, and sends its saved workspace path to the agent. Images continue to
travel inline. The same flow works by dropping files anywhere in the chat pane
below the session toolbar. The composer displays both attachment types in light
and dark mode.

## Preconditions

- The dashboard is running with a connected chat session in a local workspace.
- Chat WebSocket records and upload responses are controlled fixtures, so no model service is needed.

## Verifications

- Attach opens a picker that accepts a non-image file and an image together.
- Dragging files over the conversation, a tool row, empty transcript space, or the composer shows one outline around the whole chat pane and a centered "Drop files to attach" prompt.
- Moving between those regions does not clear the prompt; the session toolbar remains outside the drop target.
- Dropping an image and non-image over transcript content produces the same thumbnail and filename chip as Attach, without sending or uploading twice.
- The drop prompt fits a narrow viewport in both light and dark themes without moving the transcript or composer.
- Disabled or busy drops do not navigate, upload, or queue files; text drops retain native behavior.
- The file upload request carries the original filename and exact raw bytes to the current workspace's attachment endpoint.
- The composer shows the non-image filename and image thumbnail.
- Reloading restores the draft's file reference and image without uploading the file again.
- Sending includes the saved file path in the text and the image in the image payload.
- Sending clears both attachment chips.
