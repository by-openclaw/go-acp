package consumer

import (
	"context"
	"fmt"

	"dhs/internal/rcp/codec"
)

// A local source or destination is either configured locally or linked
// to a federation one: the "Federation" column of the router
// configuration, federationUid on the wire, 0 when not linked. Moving a
// resource between local and federated is ONE update of that field — no
// delete, no create.
//
// The plan names the federation resource by its mnemonic (PlanIO.Federation);
// its id is read at run time. A linked resource takes its mnemonic,
// levels, tags and tie line settings from the federation one, so:
//
//   - a resource is unlinked BEFORE the collections are converged, so
//     that what the plan states for it locally can be applied;
//   - a resource is linked AFTER them, once the federation resources of
//     the same plan exist;
//   - a linked resource is recognised by its link as well as by its
//     mnemonic, which the server may have replaced.

// linkedTo returns the local IO linked to a federation id.
func linkedTo(ios []codec.IO, fedID int64) (codec.IO, bool) {
	if fedID == 0 {
		return codec.IO{}, false
	}
	for _, io := range ios {
		if io.FederationUID == fedID {
			return io, true
		}
	}
	return codec.IO{}, false
}

// wantsLink reports whether a planned IO states a federation link, and
// whether that statement is "linked" (true) or "local" (false).
func wantsLink(w PlanIO) (stated, linked bool) {
	if w.Federation == nil {
		return false, false
	}
	return true, *w.Federation != ""
}

// links converges the federation link of the planned local sources and
// destinations. link selects the phase: false unlinks the resources the
// plan states as local, true links those it states as federated.
func (e *ensurer) links(ctx context.Context, plan Plan, link bool) error {
	for _, s := range []struct {
		name     string
		col, fed codec.Collection
		ios      []PlanIO
	}{
		{"sources", codec.Sources, codec.FederationSources, plan.Sources},
		{"destinations", codec.Destinations, codec.FederationDestinations, plan.Destinations},
	} {
		var todo []PlanIO
		for _, w := range s.ios {
			if stated, linked := wantsLink(w); stated && linked == link {
				todo = append(todo, w)
			}
		}
		if len(todo) == 0 {
			continue
		}
		local, err := e.load(ctx, s.col)
		if err != nil {
			return err
		}
		var feds []codec.IO
		if link {
			if feds, err = e.load(ctx, s.fed); err != nil {
				return err
			}
		}
		for _, w := range todo {
			target := s.name + " " + w.Mnemonic
			var want int64
			if link {
				f, ok := find(feds, *w.Federation, false)
				if !ok {
					// The federation resource is not there: in a dry run it
					// may be created by the same plan; on a system with no
					// federation it never is.
					if e.opts.Check {
						e.note(target, "link", fmt.Sprintf("to federation %q (not there yet)", *w.Federation))
						continue
					}
					if e.opts.FederationOptional {
						e.report.Pending = append(e.report.Pending, target+" link")
						continue
					}
					return fmt.Errorf("%s: federation %s %q does not exist", target, s.name, *w.Federation)
				}
				want = f.ID
			}
			cur, ok := find(local, w.Mnemonic, false)
			if !ok {
				cur, ok = linkedTo(local, want)
			}
			if !ok {
				if e.opts.Check {
					e.note(target, "link", fmt.Sprintf("to federation %q", *w.Federation))
					continue
				}
				return fmt.Errorf("%s: no such local resource to %s", target, map[bool]string{true: "link", false: "unlink"}[link])
			}
			if cur.FederationUID == want {
				continue
			}
			if link {
				e.note(target, "link", fmt.Sprintf("to federation %q (id %d)", *w.Federation, want))
			} else {
				e.note(target, "unlink", fmt.Sprintf("from federation id %d", cur.FederationUID))
			}
			if e.opts.Check {
				continue
			}
			id := fmt.Sprint(cur.ID)
			if err := e.c.Update(ctx, s.col, id, codec.Update{FederationUID: &want}); err != nil {
				return fmt.Errorf("%s: %w", target, err)
			}
			var got int64
			done, err := e.settle(ctx, func() (bool, error) {
				now, err := e.c.Get(ctx, s.col, id)
				if err != nil {
					return false, err
				}
				got = now.FederationUID
				return got == want, nil
			})
			if err != nil {
				return err
			}
			if !done {
				return fmt.Errorf("%s: the server accepted federation %d and the resource still reads %d", target, want, got)
			}
		}
	}
	return nil
}
