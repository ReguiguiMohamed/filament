package schema

import "github.com/galaxy-io/filament/rowmodel"

type PolicyMode string

const (
	Strict     PolicyMode = "strict"
	Compatible PolicyMode = "compatible"
)

type Policy struct {
	Mode                  PolicyMode // empty defaults to strict
	AllowNullableAdd      bool
	AllowTypeWidening     bool
	AllowRelaxNullability bool
}

type Permission string

const (
	Allowed  Permission = "allowed"
	Denied   Permission = "denied"
	Unproven Permission = "unproven"
)

type PolicyDecision struct {
	Permission Permission
	Reasons    []string
}

type TypeRelation string

const (
	TypeEquivalent   TypeRelation = "equivalent"
	TypeWidening     TypeRelation = "widening"
	TypeIncompatible TypeRelation = "incompatible"
)

// TypeProof is a connector's assertion for these exact fields. Core never
// guesses native aliases or engine-specific widening from logical types alone.
// Supplying it does not prove destination exclusion or historical replay safety.
type TypeProof struct {
	Before, After rowmodel.Field
	Relation      TypeRelation
}

// EvaluatePolicy authorizes change categories only. Even Allowed needs separate
// decode, projection, write, and resume evidence before activation. Evaluate a
// complete Compare/Observe diff, not a hand-filtered subset of changes.
func EvaluatePolicy(d Diff, p Policy, proofs []TypeProof) PolicyDecision {
	result := PolicyDecision{Permission: Allowed}
	add := func(permission Permission, reason string) {
		result.Reasons = append(result.Reasons, reason)
		if permission == Denied || result.Permission == Allowed {
			result.Permission = permission
		}
	}
	if p.Mode != "" && p.Mode != Strict && p.Mode != Compatible {
		return PolicyDecision{Permission: Denied, Reasons: []string{"unknown schema policy mode"}}
	}
	if len(d.Changes) == 0 {
		return result
	}
	if p.Mode != Compatible {
		return PolicyDecision{Permission: Denied, Reasons: []string{"strict policy rejects schema changes"}}
	}
	for _, change := range d.Changes {
		switch change.Kind {
		case AddField:
			if change.After == nil || !change.After.Nullable || !p.AllowNullableAdd {
				add(Denied, "field "+change.Field+": only explicitly allowed nullable additions may proceed")
			} else if change.After.Logical == rowmodel.LogicalUnknown {
				add(Unproven, "field "+change.Field+": added type is unknown")
			}
		case ChangeNullability:
			if change.Before == nil || change.After == nil || change.Before.Nullable || !change.After.Nullable || !p.AllowRelaxNullability {
				add(Denied, "field "+change.Field+": nullability change is not allowed")
			}
		case ChangeType:
			if !p.AllowTypeWidening {
				add(Denied, "field "+change.Field+": type changes are not allowed")
				continue
			}
			relation := TypeRelation("")
			conflict := false
			matched := false
			for _, proof := range proofs {
				if change.Before != nil && change.After != nil && proof.Before == *change.Before && proof.After == *change.After {
					if matched && relation != proof.Relation {
						conflict = true
					}
					matched = true
					relation = proof.Relation
				}
			}
			if conflict {
				add(Unproven, "field "+change.Field+": conflicting type proofs")
				continue
			}
			switch relation {
			case TypeEquivalent, TypeWidening:
			case TypeIncompatible:
				add(Denied, "field "+change.Field+": incompatible type change")
			default:
				add(Unproven, "field "+change.Field+": type compatibility requires connector proof")
			}
		case ChangeOrder:
			// No destructive DDL is authorized. A positional decoder may need a
			// transition, which is a separate compatibility obligation.
		default:
			add(Denied, "unsupported automatic change: "+string(change.Kind))
		}
	}
	return result
}

type CompatibilityStatus string

const (
	Unknown            CompatibilityStatus = "unknown"
	ProvenCompatible   CompatibilityStatus = "compatible"
	TransitionRequired CompatibilityStatus = "transition_required"
	Blocked            CompatibilityStatus = "blocked"
)

type CompatibilityResult struct {
	Status CompatibilityStatus
	Reason string
}

// Assessment keeps independent obligations visible. Zero/missing or unrecognized
// evidence is unknown. Schema equality alone proves neither the physical write
// contract nor cursor/snapshot replay compatibility.
type Assessment struct {
	Decode  CompatibilityResult
	Project CompatibilityResult
	Write   CompatibilityResult
	Resume  CompatibilityResult
}

// Status combines evidence conservatively: blocked > unknown > transition >
// compatible. TransitionRequired is not activation readiness.
func (a Assessment) Status() CompatibilityStatus {
	status := ProvenCompatible
	for _, r := range []CompatibilityResult{a.Decode, a.Project, a.Write, a.Resume} {
		switch r.Status {
		case Blocked:
			return Blocked
		case ProvenCompatible:
		case TransitionRequired:
			if status == ProvenCompatible {
				status = TransitionRequired
			}
		default:
			status = Unknown
		}
	}
	return status
}
