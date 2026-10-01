package contracts

// MarkdownSave is the only client → server frame on /ws/markdown/{workspaceId}/{path}.
// Base is the text the draft was edited from; Draft is the editor's text now.
// ID is client-generated and unique per save so a resend after a dropped
// socket is not applied twice.
type MarkdownSave struct {
	Type  string `json:"type"` // always "save"
	ID    string `json:"id"`
	Base  string `json:"base"`
	Draft string `json:"draft"`
}

// MarkdownDocument is the only server → client frame. Content is the whole
// file. Revision is "sha256:" plus the lowercase hex digest of Content's bytes.
// Reply carries the id of the save this message answers and is present only
// on the requester's copy of that message.
type MarkdownDocument struct {
	Type     string `json:"type"` // always "document"
	Content  string `json:"content"`
	Revision string `json:"revision"`
	Reply    string `json:"reply,omitempty"`
}
