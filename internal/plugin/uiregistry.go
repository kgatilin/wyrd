package plugin

import "sort"

// UIRegistry indexes plugin-contributed UIComponents by (view, slot)
// so browser host pages can ask "what should I render at
// dashboard/main?" with a single lookup. Construct it once during
// bootstrap from BootstrapResult.UIComponents and treat the result as
// read-only — the host queries it from many request goroutines.
type UIRegistry struct {
	// entries[view][slot] = ordered list of mounted components.
	entries map[string]map[string][]UIRegistryEntry

	// scripts holds, in registration order, every (plugin, entry)
	// pair that should be injected as a <script defer> tag on a host
	// page. The same plugin script appears at most once even when
	// the plugin embeds multiple custom elements from one bundle.
	scripts []UIRegistryScript
}

// UIRegistryEntry is one mount: a plugin's custom element with the
// label and (resolved) asset URL needed to render it inline.
type UIRegistryEntry struct {
	Plugin   string // manifest name
	Element  string // custom-element tag name
	Label    string // EmbedSlot.Label (free-form, used for tabs)
	ModelURL string // suggested data-model-url; "" if plugin has no HTTP handler
}

// UIRegistryScript is a script tag the host page must inject so the
// browser registers the plugin's custom element. Plugins that declare
// no Assets / Entry are skipped.
type UIRegistryScript struct {
	Plugin string // manifest name
	URL    string // /plugins/<plugin-name>/assets/<entry>
}

// BuildUIRegistry builds the registry from the BootstrapResult's
// UIComponents. EmbedSlot entries with an unknown View or Slot are
// dropped; the caller can detect this by inspecting Lookup before
// rendering. ModelURL is taken verbatim from UIComponent.ModelURL when
// the plugin author set it; otherwise it falls back to
// /api/plugins/<plugin-name> when the plugin contributes any HTTP
// handler. The fallback only matches a handler mounted at Path = ""
// or "/" — plugins whose handler lives at a non-root path (e.g.
// "/scores") must set UIComponent.ModelURL explicitly.
func BuildUIRegistry(res BootstrapResult) *UIRegistry {
	reg := &UIRegistry{
		entries: make(map[string]map[string][]UIRegistryEntry),
	}

	httpPlugins := make(map[string]struct{})
	for _, h := range res.HTTPHandlers {
		if h.Handler.Handler != nil {
			httpPlugins[h.Plugin] = struct{}{}
		}
	}

	scriptSeen := make(map[string]struct{})
	for _, c := range res.UIComponents {
		comp := c.Component
		if comp.Element == "" {
			continue
		}
		modelURL := comp.ModelURL
		if modelURL == "" {
			if _, ok := httpPlugins[c.Plugin]; ok {
				modelURL = PluginAPIPrefix + c.Plugin
			}
		}
		for _, slot := range comp.EmbedAt {
			if !validView(slot.View) || !validSlot(slot.Slot) {
				continue
			}
			byView, ok := reg.entries[slot.View]
			if !ok {
				byView = make(map[string][]UIRegistryEntry)
				reg.entries[slot.View] = byView
			}
			byView[slot.Slot] = append(byView[slot.Slot], UIRegistryEntry{
				Plugin:   c.Plugin,
				Element:  comp.Element,
				Label:    slot.Label,
				ModelURL: modelURL,
			})
		}
		// Schedule the asset script once per plugin.
		if comp.Entry == "" {
			continue
		}
		if _, ok := scriptSeen[c.Plugin]; ok {
			continue
		}
		scriptSeen[c.Plugin] = struct{}{}
		reg.scripts = append(reg.scripts, UIRegistryScript{
			Plugin: c.Plugin,
			URL:    PluginAssetPrefix + c.Plugin + "/assets/" + comp.Entry,
		})
	}

	// Sort script tags by plugin name for deterministic HTML output.
	sort.Slice(reg.scripts, func(i, j int) bool { return reg.scripts[i].Plugin < reg.scripts[j].Plugin })
	return reg
}

// Lookup returns the ordered list of components registered at
// (view, slot). Returns nil when nothing is registered. The slice is
// returned by value to keep the registry effectively read-only.
func (r *UIRegistry) Lookup(view, slot string) []UIRegistryEntry {
	if r == nil {
		return nil
	}
	byView, ok := r.entries[view]
	if !ok {
		return nil
	}
	src := byView[slot]
	if len(src) == 0 {
		return nil
	}
	out := make([]UIRegistryEntry, len(src))
	copy(out, src)
	return out
}

// Scripts returns every <script defer> tag the host page should
// inject. The slice is sorted by plugin name and de-duplicated.
func (r *UIRegistry) Scripts() []UIRegistryScript {
	if r == nil {
		return nil
	}
	out := make([]UIRegistryScript, len(r.scripts))
	copy(out, r.scripts)
	return out
}

// ScriptsFor returns the scripts that belong to plugins which actually
// contribute components for view. Pages that only embed one or two
// slots use this to avoid loading every plugin's bundle on every page.
func (r *UIRegistry) ScriptsFor(view string) []UIRegistryScript {
	if r == nil {
		return nil
	}
	byView, ok := r.entries[view]
	if !ok {
		return nil
	}
	used := make(map[string]struct{})
	for _, entries := range byView {
		for _, e := range entries {
			used[e.Plugin] = struct{}{}
		}
	}
	if len(used) == 0 {
		return nil
	}
	out := make([]UIRegistryScript, 0, len(used))
	for _, s := range r.scripts {
		if _, ok := used[s.Plugin]; ok {
			out = append(out, s)
		}
	}
	return out
}

// Views returns the sorted set of views with at least one mounted
// component. Useful for host pages that build their layout from the
// registry rather than from a hard-coded view list.
func (r *UIRegistry) Views() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.entries))
	for v := range r.entries {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func validView(v string) bool {
	switch v {
	case ViewDashboard, ViewLayers, ViewPackages, ViewPackageDetail,
		ViewTypeDetail, ViewDiff, ViewTargets:
		return true
	}
	return false
}

func validSlot(s string) bool {
	switch s {
	case SlotMain, SlotSidePanel, SlotExtraTab, SlotHeaderWidget:
		return true
	}
	return false
}
