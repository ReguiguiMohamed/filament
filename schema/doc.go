// Package schema implements pure schema encoding, diffing, observation assessment,
// evolution policy, and reconciliation state transitions. It depends only on
// rowmodel and the standard library.
// It performs no discovery, destination effects, checkpoint advancement, or
// execution lifecycle management. Policy permission is not compatibility proof.
// State reducers require an adapter to enforce ownership and atomically persist
// their results. They do not implement destination fencing or a durable store.
package schema
