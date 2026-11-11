package desktop

import (
	"context"
	"errors"
	"slices"
)

// OpenKnowledgeDirectory opens a selected library after fresh authorization.
func (a *App) OpenKnowledgeDirectory(sessionID, libraryID string) error {
	return wireError(a.openKnowledgeDirectory(sessionID, libraryID, openBrowser))
}

func (a *App) openKnowledgeDirectory(sessionID, libraryID string, launch func(string) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), knowledgeRequestTimeout)
	defer cancel()
	e, err := a.explicitKnowledgeEntry(sessionID)
	if err != nil {
		return err
	}
	if err := a.checkKnowledgeAccount(e); err != nil {
		return err
	}
	mode, _ := knowledgeMode(e)
	if mode != "real" || !validRealKnowledgeID(libraryID) {
		return errKnowledgeUnavailable
	}
	e.questionMu.Lock()
	disk, err := readKnowledge(e.workspace, e.id)
	e.questionMu.Unlock()
	if err != nil {
		return err
	}
	scope := knowledgeScope(e)
	if !slices.Contains(disk.Selections[scope].LibraryIDs, libraryID) {
		return errors.New("请先选择此知识库")
	}
	library, err := a.knowledgeDetail(ctx, e, libraryID)
	if err != nil {
		return err
	}
	if !validKnowledgeDirectoryURL(library.DirectoryURL, libraryID) {
		return errors.New("知识库目录地址不可用")
	}

	// No lifecycle or selection lock spans the authorization request. Recheck
	// the current entry, account and durable selection at the OS handoff.
	a.startMu.Lock()
	defer a.startMu.Unlock()
	e.questionMu.Lock()
	defer e.questionMu.Unlock()
	disk, err = readKnowledge(e.workspace, e.id)
	if err != nil {
		return err
	}
	if !slices.Contains(disk.Selections[scope].LibraryIDs, libraryID) {
		return errors.New("知识库选择已变化，请重新选择")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.entryLocked(e.id) != e || e.closed {
		return errors.New("知识库会话已变化，请重新打开会话")
	}
	if a.tokens == nil || !a.tokens.mu.TryLock() {
		return errKnowledgeUnavailable
	}
	defer a.tokens.mu.Unlock()
	if a.tokens.identity != e.knowledge.Epoch || tokenIdentity(a.tokens.ts.AccessToken) != e.knowledge.Account {
		return errors.New("知识库会话账号已变化，请重新打开会话")
	}
	if ctx.Err() != nil {
		return errKnowledgeUnavailable
	}
	if err := launch(library.DirectoryURL); err != nil {
		return errors.New("无法打开知识库目录，请稍后重试")
	}
	return nil
}
