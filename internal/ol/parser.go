package ol

import (
	"encoding/json"

	"relentless-bookworm/internal/models"
)

// ParseBookNode parses Open Library edition JSON into a BookNode.
func ParseBookNode(body []byte) (models.BookNode, error) {
	type keyRef struct {
		Key string `json:"key"`
	}
	type edition struct {
		Key         string              `json:"key"`
		Title       string              `json:"title"`
		FullTitle   string              `json:"full_title"`
		Authors     []keyRef            `json:"authors"`
		Works       []keyRef            `json:"works"`
		Subjects    []string            `json:"subjects"`
		Publishers  []string            `json:"publishers"`
		PublishDate string              `json:"publish_date"`
		Languages   []keyRef            `json:"languages"`
		Identifiers map[string][]string `json:"identifiers"`
		Covers      []int               `json:"covers"`
	}

	var payload edition
	if err := json.Unmarshal(body, &payload); err != nil {
		return models.BookNode{}, err
	}

	node := models.BookNode{
		Key:         payload.Key,
		Title:       payload.Title,
		FullTitle:   payload.FullTitle,
		Subjects:    payload.Subjects,
		Publishers:  payload.Publishers,
		PublishDate: payload.PublishDate,
		Identifiers: payload.Identifiers,
		Covers:      payload.Covers,
		RawJSON:     body,
	}

	for _, author := range payload.Authors {
		if author.Key != "" {
			node.Authors = append(node.Authors, author.Key)
		}
	}
	for _, work := range payload.Works {
		if work.Key != "" {
			node.Works = append(node.Works, work.Key)
		}
	}
	for _, lang := range payload.Languages {
		if lang.Key != "" {
			node.Languages = append(node.Languages, lang.Key)
		}
	}

	if node.Key == "" {
		return models.BookNode{}, nil
	}
	return node, nil
}

// ParseWorkNode parses Open Library work JSON into a WorkNode.
func ParseWorkNode(body []byte) (models.WorkNode, error) {
	type authorRef struct {
		Author struct {
			Key string `json:"key"`
		} `json:"author"`
	}

	type work struct {
		Key           string      `json:"key"`
		Title         string      `json:"title"`
		Authors       []authorRef `json:"authors"`
		Subjects      []string    `json:"subjects"`
		SubjectPeople []string    `json:"subject_people"`
		SubjectTimes  []string    `json:"subject_times"`
		Description   any         `json:"description"`
		Covers        []int       `json:"covers"`
	}

	var payload work
	if err := json.Unmarshal(body, &payload); err != nil {
		return models.WorkNode{}, err
	}

	node := models.WorkNode{
		Key:           payload.Key,
		Title:         payload.Title,
		Subjects:      payload.Subjects,
		SubjectPeople: payload.SubjectPeople,
		SubjectTimes:  payload.SubjectTimes,
		Description:   textValue(payload.Description),
		Covers:        payload.Covers,
		RawJSON:       body,
	}

	for _, author := range payload.Authors {
		if author.Author.Key != "" {
			node.Authors = append(node.Authors, author.Author.Key)
		}
	}

	if node.Key == "" {
		return models.WorkNode{}, nil
	}
	return node, nil
}

// ParseAuthorNode parses Open Library author JSON into an AuthorNode.
func ParseAuthorNode(body []byte) (models.AuthorNode, error) {
	type link struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	type author struct {
		Key            string            `json:"key"`
		Name           string            `json:"name"`
		PersonalName   string            `json:"personal_name"`
		Bio            any               `json:"bio"`
		AlternateNames []string          `json:"alternate_names"`
		Photos         []int             `json:"photos"`
		RemoteIDs      map[string]string `json:"remote_ids"`
		Links          []link            `json:"links"`
	}

	var payload author
	if err := json.Unmarshal(body, &payload); err != nil {
		return models.AuthorNode{}, err
	}

	node := models.AuthorNode{
		Key:            payload.Key,
		Name:           payload.Name,
		PersonalName:   payload.PersonalName,
		Bio:            textValue(payload.Bio),
		AlternateNames: payload.AlternateNames,
		Photos:         payload.Photos,
		RemoteIDs:      payload.RemoteIDs,
		RawJSON:        body,
	}
	for _, l := range payload.Links {
		node.Links = append(node.Links, models.AuthorLink{Title: l.Title, URL: l.URL})
	}

	if node.Key == "" {
		return models.AuthorNode{}, nil
	}
	return node, nil
}

// ParseNode inspects the type and returns the appropriate node.
func ParseNode(body []byte) (models.Node, error) {
	var envelope struct {
		Type struct {
			Key string `json:"key"`
		} `json:"type"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}

	switch envelope.Type.Key {
	case "/type/edition":
		return ParseBookNode(body)
	case "/type/work":
		return ParseWorkNode(body)
	case "/type/author":
		return ParseAuthorNode(body)
	default:
		return nil, nil
	}
}

func textValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]any:
		if raw, ok := v["value"].(string); ok {
			return raw
		}
	}
	return ""
}

// ParseSearchResponse parses Open Library search JSON.
func ParseSearchResponse(body []byte) (models.SearchResponse, error) {
	var resp models.SearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return models.SearchResponse{}, err
	}
	return resp, nil
}
