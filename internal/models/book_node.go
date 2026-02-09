package models

// BookNode represents a parsed Open Library edition.
type BookNode struct {
	Key         string              `json:"key"`
	Title       string              `json:"title,omitempty"`
	FullTitle   string              `json:"full_title,omitempty"`
	Authors     []string            `json:"authors,omitempty"`
	Works       []string            `json:"works,omitempty"`
	Subjects    []string            `json:"subjects,omitempty"`
	Publishers  []string            `json:"publishers,omitempty"`
	PublishDate string              `json:"publish_date,omitempty"`
	Languages   []string            `json:"languages,omitempty"`
	Identifiers map[string][]string `json:"identifiers,omitempty"`
	Covers      []int               `json:"covers,omitempty"`
	RawJSON     []byte              `json:"-"`
}

func (b BookNode) NodeType() NodeType {
	return NodeTypeEdition
}

func (b BookNode) ID() string {
	return b.Key
}
