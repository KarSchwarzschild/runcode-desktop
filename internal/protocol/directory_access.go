package protocol

// DirectoryAccessRequest names the calling session, never the focus.
type DirectoryAccessRequest struct {
	ConfirmRoot bool   `json:"confirmRoot,omitempty"`
	SessionID   string `json:"sessionId"`
	Directory   string `json:"directory"`
	Access      string `json:"access"`
	Scope       string `json:"scope"`
}

// DirectoryAccessGrant is one scoped directory allow rule.
type DirectoryAccessGrant struct {
	Directory string   `json:"directory"`
	Access    string   `json:"access"`
	Scope     string   `json:"scope"`
	CoveredBy []string `json:"coveredBy"`
}

// DirectoryAccessView lists directory rules for a named session.
type DirectoryAccessView struct {
	SessionID string                 `json:"sessionId"`
	Workspace string                 `json:"workspace"`
	Revision  string                 `json:"revision"`
	Grants    []DirectoryAccessGrant `json:"grants"`
}

// ArtifactReference is a bounded, session-instance-bound file capability.
type ArtifactReference struct {
	Reference  string `json:"reference"`
	SessionID  string `json:"sessionId"`
	Generation string `json:"generation"`
	Path       string `json:"path"`
	URL        string `json:"url"`
}

// DirectoryAccessChanged invalidates the directory view for one workspace.
type DirectoryAccessChanged struct {
	Workspace string `json:"workspace"`
}
