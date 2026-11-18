//go:build darwin

package main

import (
	"strings"

	"focus.local/zhuanzhuli/internal/session"
	"github.com/egoist/mygo"
)

// Only an explicitly listed application failure can be accepted. Storage,
// notification and recovery failures never inherit application consent.
type protectionError struct {
	kind, key string
	cause     error
}

func (e *protectionError) Error() string { return e.kind + "：" + e.cause.Error() }
func (e *protectionError) Unwrap() error { return e.cause }
func protectionIssue(kind, key string, err error) error {
	if err == nil {
		return nil
	}
	return &protectionError{kind, key, err}
}
func protectionLeaves(err error) []error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var out []error
		for _, child := range joined.Unwrap() {
			out = append(out, protectionLeaves(child)...)
		}
		return out
	}
	return []error{err}
}
func consentedApplications(err error, consent map[string]bool) bool {
	for _, leaf := range protectionLeaves(err) {
		issue, ok := leaf.(*protectionError)
		if !ok || issue.kind != "应用限制" || issue.key == "" || !consent[issue.key] {
			return false
		}
	}
	return true
}
func (a *application) appProtectionError() string {
	var details []string
	for _, leaf := range protectionLeaves(a.protectionFailure) {
		if issue, ok := leaf.(*protectionError); ok && issue.kind == "应用限制" {
			details = append(details, issue.Error())
		}
	}
	return strings.Join(details, "\n")
}
func (a *application) applicationConsent() map[string]bool {
	consent := map[string]bool{}
	for _, leaf := range protectionLeaves(a.protectionFailure) {
		if issue, ok := leaf.(*protectionError); ok && issue.kind == "应用限制" {
			consent[issue.key] = true
		}
	}
	return consent
}

type protectionPlan struct {
	id           string
	generation   uint64
	resume       bool
	allowed      []string
	consent      map[string]bool
	focusLinked  bool
	focusProfile string
	immersion    bool
	debugAttempt string
}

func (a *application) newProtectionPlan(resume bool, allowed []string, consent map[string]bool) *protectionPlan {
	a.protectionGeneration++
	prefs := a.store.Data.Preferences
	accepted := map[string]bool{}
	for key, value := range consent {
		accepted[key] = value
	}
	s := a.controller.Snapshot()
	p := &protectionPlan{id: s.ID, generation: a.protectionGeneration, resume: resume, allowed: append([]string{}, allowed...), consent: accepted, immersion: s.Immersion, debugAttempt: a.debugAttempt}
	if !s.Immersion {
		p.focusLinked, p.focusProfile = prefs.FocusLinked, prefs.FocusProfile
	}
	a.preparing = p
	return p
}
func (a *application) planValid(p *protectionPlan) bool {
	s := a.controller.Snapshot()
	return s.ID == p.id && a.protectionGeneration == p.generation && !a.quitting &&
		((p.resume && s.Phase == "paused") || (!p.resume && (s.Phase == "entry" || session.Working(s.Phase))))
}
func (a *application) preparationAllowed() bool {
	if a.preparing != nil {
		return a.planValid(a.preparing)
	}
	p := a.controller.Snapshot().Phase
	return p == "entry" || session.Working(p)
}
func (a *application) preparationValid(p *protectionPlan) bool {
	var valid bool
	mygo.RunOnMain(func() { valid = a.planValid(p) })
	return valid
}

func (a *application) protectionStopped(err error) {
	a.diagnostic("preparation", "failed")
	a.protectionFailure = err
	a.controller.Pause()
	a.stopEnvironment()
	if !a.persist() {
		return
	}
	a.notice = Notice{noticeProtection, "环境准备未完成，计时保持暂停。查看详情后重试，或结束专注。"}
	a.renderMain()
	a.openMain()
	a.effect(a.restore, nil)
}
