package models

// SearchDoc represents a minimal Open Library search result.
type SearchDoc struct {
	Key               string   `json:"key"`
	Title             string   `json:"title,omitempty"`
	AuthorKey         []string `json:"author_key,omitempty"`
	AuthorName        []string `json:"author_name,omitempty"`
	CoverEditionKey   string   `json:"cover_edition_key,omitempty"`
	CoverI            int      `json:"cover_i,omitempty"`
	EbookAccess       string   `json:"ebook_access,omitempty"`
	EditionCount      int      `json:"edition_count,omitempty"`
	FirstPublishYear  int      `json:"first_publish_year,omitempty"`
	HasFulltext       bool     `json:"has_fulltext,omitempty"`
	IA                []string `json:"ia,omitempty"`
	IACollection      []string `json:"ia_collection,omitempty"`
	Language          []string `json:"language,omitempty"`
	LendingEdition    string   `json:"lending_edition_s,omitempty"`
	LendingIdentifier string   `json:"lending_identifier_s,omitempty"`
	PublicScanB       bool     `json:"public_scan_b,omitempty"`
	IDStandardEbooks  []string `json:"id_standard_ebooks,omitempty"`
	Editions          struct {
		Docs []struct {
			Key string `json:"key"`
		} `json:"docs"`
	} `json:"editions"`
}

// SearchResponse represents the Open Library search response.
type SearchResponse struct {
	NumFound         int         `json:"numFound,omitempty"`
	NumFoundExact    bool        `json:"numFoundExact,omitempty"`
	NumFoundAlt      int         `json:"num_found,omitempty"`
	Start            int         `json:"start,omitempty"`
	Q                string      `json:"q,omitempty"`
	Offset           *int        `json:"offset,omitempty"`
	DocumentationURL string      `json:"documentation_url,omitempty"`
	Docs             []SearchDoc `json:"docs"`
}
