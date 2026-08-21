package plugin

import "context"

// A daemon serving one repository serves every worktree of it, and the
// answer to "what does this project's event model look like" is different
// in each. The Host a plugin receives at Init can only name one of them:
// bootstrap runs once, before any request exists.
//
// So the Host a plugin answers from is a property of the call, not of the
// plugin. wyrd puts the calling worktree's Host on the request context and
// plugins read it back here, falling back to the bootstrap Host when the
// call did not come from a worktree-scoped surface (the CLI, a one-shot
// stdio session) and there is only one model to answer from anyway.

type hostCtxKey struct{}

// ContextWithHost returns a copy of ctx carrying host as the Host that
// plugin capabilities invoked under it should answer from. A nil host
// returns ctx unchanged, so callers need not special-case it.
func ContextWithHost(ctx context.Context, host Host) context.Context {
	if host == nil {
		return ctx
	}
	return context.WithValue(ctx, hostCtxKey{}, host)
}

// HostFromContext returns the Host scoped to this call, or nil when the
// call carries none. Plugins should prefer it over the Init-time Host and
// fall back to that one when it returns nil.
func HostFromContext(ctx context.Context) Host {
	if ctx == nil {
		return nil
	}
	host, _ := ctx.Value(hostCtxKey{}).(Host)
	return host
}
