package ui

import (
	"net/http"
	"sort"
)

// rotationRow is a secret flagged for rotation.
type rotationRow struct {
	Key       string
	Env       string
	ScopePath string
	Reason    string
	FlaggedAt string
	Guidance  string
	RevokeURL string
}

func (s *Server) handleStoreRotation(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, err := st.ResolveDefaultProject()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var rows []rotationRow
	allScopes, _ := st.ListAllScopes(project)
	for _, scope := range allScopes {
		manifest := readManifestFile(st.Root, project, scope.Path)
		if manifest == nil || len(manifest.RotationFlags) == 0 {
			continue
		}

		env := envFromScope(scope.Path)
		for key, flag := range manifest.RotationFlags {
			row := rotationRow{
				Key:       key,
				Env:       env,
				ScopePath: scope.Path,
				Reason:    flag.Reason,
				FlaggedAt: timeAgo(flag.FlaggedAt),
			}

			if provName, ok := manifest.Providers[key]; ok {
				prov := s.registry.Get(provName)
				if prov != nil {
					if prov.Rotation.Strategy != "" {
						row.Guidance = prov.Rotation.Strategy
						if prov.Rotation.Warning != "" {
							row.Guidance += " — " + prov.Rotation.Warning
						}
					}
					row.RevokeURL = prov.GetRevokeURL()
				}
			}

			rows = append(rows, row)
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })

	s.render(w, "store_rotation.html", map[string]any{
		"Page":      "stores",
		"Tab":       "rotation",
		"StoreName": name,
		"StoreType":   st.Meta.Type,
		"StoreRemote": storeRemote(st),
		"Rows":      rows,
		"Count":     len(rows),
	})
}

func (s *Server) handleRotationClear(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	scope := r.FormValue("scope")
	key := r.FormValue("key")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, _ := st.ResolveDefaultProject()

	manifest := readManifestFile(st.Root, project, scope)
	if manifest != nil && manifest.RotationFlags != nil {
		delete(manifest.RotationFlags, key)
		writeManifestFile(st.Root, project, scope, manifest)
	}

	if isHTMX(r) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<span class="text-emerald-400 text-sm">Cleared</span>`))
		return
	}

	http.Redirect(w, r, "/stores/"+name+"/rotation", http.StatusFound)
}

// storeRotationCount returns the total rotation flags for a store (used by stores list).
func storeRotationCount(storeRoot, project string, scopes []scopeInfo) int {
	count := 0
	for _, scope := range scopes {
		manifest := readManifestFile(storeRoot, project, scope.Path)
		if manifest != nil {
			count += len(manifest.RotationFlags)
		}
	}
	return count
}

type scopeInfo struct {
	Path string
}
