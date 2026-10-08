package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/galaxy-io/filament/rowmodel"
)

// StorageFormat versions the explicit persistence representation. These records
// preserve nil/empty distinctions in exact request receipts and private schema
// flags; default JSON of State or SchemaVersion does neither.
const StorageFormat uint16 = 1

type VersionRecord struct {
	Format      uint16
	ID          rowmodel.SchemaVersionID
	Previous    rowmodel.SchemaVersionID
	Fingerprint rowmodel.SchemaFingerprint
	Model       rowmodel.SchemaData
}

func StoreVersion(v rowmodel.SchemaVersion) VersionRecord {
	return VersionRecord{StorageFormat, v.ID(), v.Previous(), v.Fingerprint(), v.Model().Data()}
}
func (r VersionRecord) Restore() (rowmodel.SchemaVersion, error) {
	if r.Format != StorageFormat {
		return rowmodel.SchemaVersion{}, fmt.Errorf("unsupported schema version storage format %d", r.Format)
	}
	model, err := rowmodel.SchemaFromData(r.Model)
	if err != nil {
		return rowmodel.SchemaVersion{}, err
	}
	v, err := NewVersion(r.ID, r.Previous, model)
	if err != nil {
		return v, err
	}
	if v.Fingerprint() != r.Fingerprint {
		return v, fmt.Errorf("schema version fingerprint mismatch")
	}
	return v, nil
}

type ObservationRecord struct {
	Resource      string
	Model         *rowmodel.SchemaData
	Evidence      EvidenceKind
	Complete      bool
	Authoritative bool
	Reference     *Reference
}

func storeObservation(o Observation) ObservationRecord {
	o = o.Clone()
	r := ObservationRecord{Resource: o.Resource, Evidence: o.Evidence, Complete: o.Complete, Authoritative: o.Authoritative, Reference: o.Reference}
	if o.Model != nil {
		d := o.Model.Data()
		r.Model = &d
	}
	return r
}
func (r ObservationRecord) restore() (Observation, error) {
	o := Observation{Resource: r.Resource, Evidence: r.Evidence, Complete: r.Complete, Authoritative: r.Authoritative, Reference: r.Reference}
	if r.Model != nil {
		m, err := rowmodel.SchemaFromData(*r.Model)
		if err != nil {
			return o, err
		}
		o.Model = &m
	}
	return o.Clone(), nil
}

type BeginRecord struct {
	Operation              OperationID
	ExpectedSchemaRevision int64
	Observation            ObservationRecord
	After                  []rowmodel.SchemaBinding
	Versions               []VersionRecord
	Plan                   *Plan
	BlockedReason          string
	Supersedes             OperationID
}

func storeBegin(r BeginRequest) BeginRecord {
	r = r.clone()
	b := BeginRecord{Operation: r.Operation, ExpectedSchemaRevision: r.ExpectedSchemaRevision, Observation: storeObservation(r.Observation), After: r.After, Plan: r.Plan, BlockedReason: r.BlockedReason, Supersedes: r.Supersedes}
	if r.Versions != nil {
		b.Versions = make([]VersionRecord, len(r.Versions))
		for i, v := range r.Versions {
			b.Versions[i] = StoreVersion(v)
		}
	}
	return b
}
func (b BeginRecord) restore() (BeginRequest, error) {
	o, err := b.Observation.restore()
	if err != nil {
		return BeginRequest{}, err
	}
	r := BeginRequest{Operation: b.Operation, ExpectedSchemaRevision: b.ExpectedSchemaRevision, Observation: o, After: b.After, Plan: b.Plan, BlockedReason: b.BlockedReason, Supersedes: b.Supersedes}
	if b.Versions != nil {
		r.Versions = make([]rowmodel.SchemaVersion, len(b.Versions))
		for i, v := range b.Versions {
			restored, err := v.Restore()
			if err != nil {
				return r, err
			}
			r.Versions[i] = restored
		}
	}
	return r.clone(), nil
}

// OperationRecord stores request history rather than trusting caller-selected
// phases. Restoration replays the reducer to validate every transition.
type OperationRecord struct {
	Format     uint16
	Begin      BeginRecord
	Advances   []AdvanceRequest
	Activation *ActivateRequest
}

func storeOperation(o Operation) OperationRecord {
	r := OperationRecord{Format: StorageFormat, Begin: storeBegin(o.begin)}
	revisions := make([]int64, 0, len(o.advances))
	for revision := range o.advances {
		revisions = append(revisions, revision)
	}
	sort.Slice(revisions, func(i, j int) bool { return revisions[i] < revisions[j] })
	for _, revision := range revisions {
		r.Advances = append(r.Advances, o.advances[revision].clone())
	}
	if o.activation != nil {
		a := *o.activation
		r.Activation = &a
	}
	return r
}

// Snapshot is a versioned, detached DTO. InitialBindings and ordered Operations
// reconstruct history; Versions are immutable content referenced by that history.
type Snapshot struct {
	Format          uint16
	InitialBindings []rowmodel.SchemaBinding
	Versions        []VersionRecord
	Operations      []OperationRecord
}

func (s State) Snapshot() Snapshot {
	snapshot := Snapshot{Format: StorageFormat, InitialBindings: s.Bindings()}
	ids := make([]string, 0, len(s.versions))
	for id := range s.versions {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	for _, id := range ids {
		snapshot.Versions = append(snapshot.Versions, StoreVersion(s.versions[rowmodel.SchemaVersionID(id)]))
	}
	// A scope has one pending operation. Within a schema revision, blocked
	// replacements form a single chain; activation advances to the next revision.
	remaining := map[OperationID]Operation{}
	for id, op := range s.operations {
		remaining[id] = op
	}
	revision := int64(1)
	var supersedes OperationID
	for len(remaining) > 0 {
		var found Operation
		ok := false
		for _, op := range remaining {
			if op.begin.ExpectedSchemaRevision == revision && op.begin.Supersedes == supersedes {
				found = op
				ok = true
				break
			}
		}
		if !ok {
			panic("schema: invalid internal operation history")
		}
		if len(snapshot.Operations) == 0 {
			snapshot.InitialBindings = found.Before()
		}
		snapshot.Operations = append(snapshot.Operations, storeOperation(found))
		delete(remaining, found.id)
		if found.phase == Activated {
			revision++
			supersedes = ""
		} else {
			supersedes = found.id
		}
	}
	return snapshot
}
func Restore(snapshot Snapshot) (State, error) {
	if snapshot.Format != StorageFormat {
		return State{}, fmt.Errorf("unsupported schema state format %d", snapshot.Format)
	}
	versions := make([]rowmodel.SchemaVersion, 0, len(snapshot.Versions))
	seen := map[rowmodel.SchemaVersionID]bool{}
	for _, r := range snapshot.Versions {
		if seen[r.ID] {
			return State{}, fmt.Errorf("duplicate stored schema version")
		}
		seen[r.ID] = true
		v, err := r.Restore()
		if err != nil {
			return State{}, err
		}
		versions = append(versions, v)
	}
	state, err := NewState(snapshot.InitialBindings, versions)
	if err != nil {
		return state, err
	}
	seenOps := map[OperationID]bool{}
	for _, r := range snapshot.Operations {
		if r.Format != StorageFormat || seenOps[r.Begin.Operation] {
			return State{}, fmt.Errorf("invalid operation record format or duplicate ID")
		}
		seenOps[r.Begin.Operation] = true
		begin, err := r.Begin.restore()
		if err != nil {
			return State{}, err
		}
		// Begin may only reference versions present in the immutable version table.
		for _, v := range begin.Versions {
			existing, ok := state.Version(v.ID())
			if !ok || existing.Previous() != v.Previous() || existing.Fingerprint() != v.Fingerprint() {
				return State{}, fmt.Errorf("operation references missing or conflicting stored version")
			}
		}
		state, err = Begin(state, begin)
		if err != nil {
			return State{}, err
		}
		for _, advance := range r.Advances {
			op, _ := state.Operation(begin.Operation)
			if advance.Operation != begin.Operation || advance.ExpectedOperationRevision != op.Revision() {
				return State{}, fmt.Errorf("invalid stored advance sequence")
			}
			state, err = Advance(state, advance)
			if err != nil {
				return State{}, err
			}
		}
		if r.Activation != nil {
			if r.Activation.Operation != begin.Operation {
				return State{}, fmt.Errorf("activation operation mismatch")
			}
			state, err = Activate(state, *r.Activation)
			if err != nil {
				return State{}, err
			}
		}
	}
	return state, nil
}

// MarshalRecord and UnmarshalRecord are strict, canonical storage JSON. Store the
// bytes as BLOB/bytea; database JSON normalization would erase exact encoding.
func MarshalRecord(v any) ([]byte, error) { return json.Marshal(v) }
func UnmarshalRecord(data []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing schema storage data")
	}
	canonical, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, canonical) {
		return fmt.Errorf("noncanonical schema storage record")
	}
	return nil
}

// StoredState is a detached database record set. Backends only map SQL rows to
// these DTOs. DecodeStored owns reconstruction and semantic integrity checks.
type StoredState struct {
	StorageRevision    int64
	Revision           int64
	PayloadVersion     int64
	InitialBytes       []byte
	BindingsBytes      []byte
	PendingOperationID string
	Versions           []StoredVersion
	Operations         []StoredOperation
	Bindings           []StoredBindings
}
type StoredVersion struct {
	VersionID         string
	Resource          string
	PreviousVersionID string
	EncodingVersion   int64
	Fingerprint       []byte
	ModelBytes        []byte
	RecordBytes       []byte
}
type StoredOperation struct {
	OperationID       string
	Sequence          int64
	Revision          int64
	Phase             string
	PayloadVersion    int64
	RecordBytes       []byte
	ActivatedRevision int64
}
type StoredBindings struct {
	Revision       int64
	PayloadVersion int64
	BindingsBytes  []byte
}

// Writes is prepared by the schema core, then conditionally persisted by a store.
// A nil Writes means an ownership-checked historical retry, without new effects.
type Writes struct {
	Revision           int64
	PayloadVersion     int64
	InitialBytes       []byte
	BindingsBytes      []byte
	PendingOperationID string
	Versions           []StoredVersion
	Operations         []StoredOperation
	Binding            *StoredBindings
}

func DecodeStored(row StoredState) (State, error) {
	if row.StorageRevision < 1 {
		return State{}, fmt.Errorf("invalid storage revision")
	}
	if row.PayloadVersion != int64(StorageFormat) {
		return State{}, fmt.Errorf("unsupported schema state payload version")
	}
	var initial Snapshot
	if err := UnmarshalRecord(row.InitialBytes, &initial); err != nil {
		return State{}, err
	}
	if _, err := Restore(initial); err != nil {
		return State{}, err
	}
	if len(initial.Operations) != 0 {
		return State{}, fmt.Errorf("initial schema state contains operations")
	}
	snapshot := Snapshot{Format: StorageFormat, InitialBindings: initial.InitialBindings}
	for _, row := range row.Versions {
		var record VersionRecord
		if err := UnmarshalRecord(row.RecordBytes, &record); err != nil {
			return State{}, err
		}
		v, err := record.Restore()
		if err != nil {
			return State{}, err
		}
		canonical, err := Encode(v.Model())
		if err != nil {
			return State{}, err
		}
		fp := v.Fingerprint()
		if string(v.ID()) != row.VersionID || v.Model().Resource != row.Resource || string(v.Previous()) != row.PreviousVersionID || int64(fp.Format) != row.EncodingVersion || !bytes.Equal(fp.SHA256[:], row.Fingerprint) || !bytes.Equal(canonical, row.ModelBytes) {
			return State{}, fmt.Errorf("schema version metadata mismatch")
		}
		snapshot.Versions = append(snapshot.Versions, record)
	}
	for i, row := range row.Operations {
		if row.PayloadVersion != int64(StorageFormat) || row.Sequence != int64(i+1) {
			return State{}, fmt.Errorf("invalid schema operation sequence or format")
		}
		var record OperationRecord
		if err := UnmarshalRecord(row.RecordBytes, &record); err != nil {
			return State{}, err
		}
		if string(record.Begin.Operation) != row.OperationID {
			return State{}, fmt.Errorf("operation identity mismatch")
		}
		snapshot.Operations = append(snapshot.Operations, record)
	}
	state, err := Restore(snapshot)
	if err != nil {
		return State{}, err
	}
	for _, v := range initial.Versions {
		actual, ok := state.Version(v.ID)
		if !ok || actual.Previous() != v.Previous || actual.Fingerprint() != v.Fingerprint {
			return State{}, fmt.Errorf("initial schema version missing or changed")
		}
	}
	for i, row := range row.Operations {
		op, _ := state.Operation(OperationID(row.OperationID))
		activated := int64(0)
		if snapshot.Operations[i].Activation != nil {
			activated = snapshot.Operations[i].Activation.ExpectedSchemaRevision + 1
		}
		if op.Revision() != row.Revision || string(op.Phase()) != row.Phase || activated != row.ActivatedRevision {
			return State{}, fmt.Errorf("operation metadata disagrees with its history")
		}
	}
	bindings, err := MarshalRecord(state.Bindings())
	if err != nil {
		return State{}, err
	}
	if state.Revision() != row.Revision || string(state.Pending()) != row.PendingOperationID || !bytes.Equal(bindings, row.BindingsBytes) {
		return State{}, fmt.Errorf("active schema state disagrees with its history")
	}

	if len(row.Bindings) != int(state.Revision()) {
		return State{}, fmt.Errorf("binding history incomplete")
	}
	for i, binding := range row.Bindings {
		if binding.Revision != int64(i+1) {
			return State{}, fmt.Errorf("invalid binding history sequence")
		}
		if _, err := DecodeBindings(state, binding); err != nil {
			return State{}, err
		}
	}
	return state, nil
}

// PrepareWrites encodes only new or changed records. Reducers must run before
// this function. It does not authorize effects or advance source progress.
func PrepareWrites(previous, next State, initialize bool) (*Writes, error) {
	snapshot := next.Snapshot()
	w := &Writes{Revision: next.Revision(), PayloadVersion: int64(StorageFormat), PendingOperationID: string(next.Pending())}
	var err error
	w.BindingsBytes, err = MarshalRecord(next.Bindings())
	if err != nil {
		return nil, err
	}
	if initialize {
		w.InitialBytes, err = MarshalRecord(snapshot)
		if err != nil {
			return nil, err
		}
	}
	for _, record := range snapshot.Versions {
		if !initialize {
			if _, ok := previous.Version(record.ID); ok {
				continue
			}
		}
		version, err := record.Restore()
		if err != nil {
			return nil, err
		}
		model, err := Encode(version.Model())
		if err != nil {
			return nil, err
		}
		data, err := MarshalRecord(record)
		if err != nil {
			return nil, err
		}
		fp := version.Fingerprint()
		w.Versions = append(w.Versions, StoredVersion{string(version.ID()), version.Model().Resource, string(version.Previous()), int64(fp.Format), fp.SHA256[:], model, data})
	}
	for i, record := range snapshot.Operations {
		op, _ := next.Operation(record.Begin.Operation)
		if old, ok := previous.Operation(op.ID()); ok && old.Revision() == op.Revision() {
			continue
		}
		data, err := MarshalRecord(record)
		if err != nil {
			return nil, err
		}
		activated := int64(0)
		if record.Activation != nil {
			activated = record.Activation.ExpectedSchemaRevision + 1
		}
		w.Operations = append(w.Operations, StoredOperation{string(op.ID()), int64(i + 1), op.Revision(), string(op.Phase()), int64(StorageFormat), data, activated})
	}
	if initialize || previous.Revision() != next.Revision() {
		w.Binding = &StoredBindings{next.Revision(), int64(StorageFormat), w.BindingsBytes}
	}
	if !initialize && len(w.Versions) == 0 && len(w.Operations) == 0 && w.Binding == nil {
		return nil, nil
	}
	return w, nil
}

func DecodeBindings(state State, row StoredBindings) ([]rowmodel.SchemaBinding, error) {
	if row.PayloadVersion != int64(StorageFormat) {
		return nil, fmt.Errorf("unsupported binding storage format")
	}
	snapshot := state.Snapshot()
	expected := snapshot.InitialBindings
	found := row.Revision == 1
	for _, r := range snapshot.Operations {
		if r.Activation != nil && r.Activation.ExpectedSchemaRevision+1 == row.Revision {
			expected = r.Begin.After
			found = true
			break
		}
	}
	canonical, err := MarshalRecord(expected)
	if err != nil {
		return nil, err
	}
	if !found || !bytes.Equal(canonical, row.BindingsBytes) {
		return nil, fmt.Errorf("historical bindings disagree with operation history")
	}
	var bindings []rowmodel.SchemaBinding
	if err := UnmarshalRecord(row.BindingsBytes, &bindings); err != nil {
		return nil, err
	}
	return bindings, nil
}
