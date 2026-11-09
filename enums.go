package clientgo

// Priority represents task priority levels, matching BitBucket values.
type Priority string

// Priority constants.
const (
	PriorityTrivial  Priority = "Trivial"
	PriorityMinor    Priority = "Minor"
	PriorityMajor    Priority = "Major"
	PriorityCritical Priority = "Critical"
	PriorityBlocker  Priority = "Blocker"
)

// IsValid checks if the priority value is one of the allowed constants.
func (p Priority) IsValid() bool {
	switch p {
	case PriorityTrivial, PriorityMinor, PriorityMajor, PriorityCritical, PriorityBlocker:
		return true
	default:
		return false
	}
}

// Kind represents the type of task, matching BitBucket values.
type Kind string

// Kind constants.
const (
	KindBug         Kind = "Bug"
	KindEnhancement Kind = "Enhancement"
	KindTask        Kind = "Task"
	KindProposal    Kind = "Proposal"
)

// IsValid checks if the kind value is one of the allowed constants.
func (k Kind) IsValid() bool {
	switch k {
	case KindBug, KindEnhancement, KindTask, KindProposal:
		return true
	default:
		return false
	}
}

// Status represents task lifecycle states, matching BitBucket values.
type Status string

// Status constants.
const (
	StatusNew        Status = "New"
	StatusOpen       Status = "Open"
	StatusInProgress Status = "In Progress"
	StatusResolved   Status = "Resolved"
	StatusClosed     Status = "Closed"
	StatusReopened   Status = "Reopened"
	StatusInvalid    Status = "Invalid"
	StatusDuplicate  Status = "Duplicate"
	StatusWontfix    Status = "Wontfix"
	StatusOnHold     Status = "On Hold"
)

// IsValid checks if the status value is one of the allowed constants.
func (s Status) IsValid() bool {
	switch s {
	case StatusNew, StatusOpen, StatusInProgress, StatusResolved, StatusClosed, StatusReopened,
		StatusInvalid, StatusDuplicate, StatusWontfix, StatusOnHold:
		return true
	default:
		return false
	}
}
