package models

// WorkNode represents a parsed Open Library work.
type WorkNode struct {
	Key           string   `json:"key"`
	Title         string   `json:"title,omitempty"`
	Authors       []string `json:"authors,omitempty"`
	Subjects      []string `json:"subjects,omitempty"`
	SubjectPeople []string `json:"subject_people,omitempty"`
	SubjectTimes  []string `json:"subject_times,omitempty"`
	Description   string   `json:"description,omitempty"`
	Covers        []int    `json:"covers,omitempty"`
	RawJSON       []byte   `json:"-"`
}

func (w WorkNode) NodeType() NodeType {
	return NodeTypeWork
}

func (w WorkNode) ID() string {
	return w.Key
}
