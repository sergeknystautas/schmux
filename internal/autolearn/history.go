//go:build !noautolearn

package autolearn

// FilterLearnings filters learnings from batches by kind, status, and/or layer.
// Pass nil for any filter to skip that criterion. The layer filter matches
// against EffectiveLayer (ChosenLayer if set, otherwise SuggestedLayer).
func FilterLearnings(batches []*Batch, kind *LearningKind, status *LearningStatus, layer *Layer) []Learning {
	var result []Learning
	for _, b := range batches {
		for _, l := range b.Learnings {
			if kind != nil && l.Kind != *kind {
				continue
			}
			if status != nil && l.Status != *status {
				continue
			}
			if layer != nil && l.EffectiveLayer() != *layer {
				continue
			}
			result = append(result, l)
		}
	}
	return result
}

// AllLearnings flattens all learnings from all batches into a single slice.
func AllLearnings(batches []*Batch) []Learning {
	var result []Learning
	for _, b := range batches {
		result = append(result, b.Learnings...)
	}
	return result
}
