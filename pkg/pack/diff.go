package pack

import "sort"

type DiffResult struct {
	Uploads []string
	Deletes []string
}

func ComputeDiff(
	local, remote Manifest,
	deleteEnabled bool,
) DiffResult {
	var result DiffResult

	for path, le := range local {
		re, exists := remote[path]
		if !exists || le.Hash != re.Hash {
			result.Uploads = append(result.Uploads, path)
		}
	}

	if deleteEnabled {
		for path := range remote {
			if _, exists := local[path]; !exists {
				result.Deletes = append(
					result.Deletes, path,
				)
			}
		}
	}

	sort.Strings(result.Uploads)
	sort.Strings(result.Deletes)
	return result
}
