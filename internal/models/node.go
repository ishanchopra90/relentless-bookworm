package models

// NodeType describes the Open Library entity type.
type NodeType string

const (
	NodeTypeEdition NodeType = "edition"
	NodeTypeWork    NodeType = "work"
	NodeTypeAuthor  NodeType = "author"
)

// Node represents a parsed Open Library entity.
type Node interface {
	NodeType() NodeType
	ID() string
}
