# Remote clipboard image paste

A user pastes an image into a remote session terminal. The image is transferred
to the remote host, set on the remote X11 clipboard via xclip, and Ctrl+V is
sent to the session pane. The agent reads the image from the clipboard,
completing the full round-trip.

## Preconditions

- The daemon is running
- A remote flavor is configured (using mock-remote.sh)
- A remote session is spawned and connected
- xclip and Xvfb are installed and running (DISPLAY=:99)
- The session runs mock-clipboard-agent.sh (reads clipboard on Ctrl+V)

## Verifications

- POST /api/clipboard-paste with a base64 image returns 200 for a remote session
- The mock agent receives Ctrl+V and reads the image from the X11 clipboard
- The agent confirms the image size matches the original (67 bytes for 1x1 PNG)
