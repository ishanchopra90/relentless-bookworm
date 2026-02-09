package models

// AuthorNode represents a parsed Open Library author.
type AuthorNode struct {
	Key            string            `json:"key"`
	Name           string            `json:"name,omitempty"`
	PersonalName   string            `json:"personal_name,omitempty"`
	Bio            string            `json:"bio,omitempty"`
	AlternateNames []string          `json:"alternate_names,omitempty"`
	Photos         []int             `json:"photos,omitempty"`
	RemoteIDs      map[string]string `json:"remote_ids,omitempty"`
	Links          []AuthorLink      `json:"links,omitempty"`
	RawJSON        []byte            `json:"-"`
}

type AuthorLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

func (a AuthorNode) NodeType() NodeType {
	return NodeTypeAuthor
}

func (a AuthorNode) ID() string {
	return a.Key
}
