package main

import "testing"

func TestOperatorWriteAtomicity(t *testing.T) {
	atomicOperations := []string{
		"events.create",
		"events.correct",
		"facts.confirm",
		"facts.revoke",
		"facts.reconcile",
	}
	for _, operation := range atomicOperations {
		if !operatorWriteIsAtomic(operation) {
			t.Errorf("operation %q should be atomic", operation)
		}
	}
	reservedOperations := []string{
		"match.reset",
		"sources.start",
		"sources.stop",
		"match.takeover",
		"automation.set",
		"config.set",
	}
	for _, operation := range reservedOperations {
		if operatorWriteIsAtomic(operation) {
			t.Errorf("operation %q should reserve before side effects", operation)
		}
	}
}
