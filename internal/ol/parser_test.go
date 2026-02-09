package ol

import (
	"testing"

	"relentless-bookworm/internal/models"
)

func TestParseBookNode(t *testing.T) {
	payload := []byte(`{
  "type": {
    "key": "/type/edition"
  },
  "authors": [
    {
      "key": "/authors/OL13066A"
    }
  ],
  "description": "<p><i>The Time Machine</i> is the novel that gave us the concept of—and even the word for—a “time machine.” While it’s not <a href=\"https://standardebooks.org/ebooks/h-g-wells\">Wells’</a> first story involving time travel, it <em>is</em> the one that most fully fleshes out the concept of a device that can send a person backwards and forwards in time with complete precision. Time machines have since become a staple of the science fiction and fantasy genres, making <i>The Time Machine</i> one of the most deeply influential science fiction novels of the era.</p>",
  "identifiers": {
    "standard_ebooks": [
      "h-g-wells/the-time-machine"
    ]
  },
  "languages": [
    {
      "key": "/languages/eng"
    }
  ],
  "publish_date": "2014",
  "publishers": [
    "Standard Ebooks"
  ],
  "source_records": [
    "standard_ebooks:h-g-wells/the-time-machine"
  ],
  "subjects": [
    "Time travel--Fiction"
  ],
  "title": "The Time Machine",
  "full_title": "The Time Machine",
  "covers": [
    15143630,
    12621679
  ],
  "works": [
    {
      "key": "/works/OL52267W"
    }
  ],
  "key": "/books/OL37044446M",
  "number_of_pages": 84,
  "latest_revision": 6,
  "revision": 6,
  "created": {
    "type": "/type/datetime",
    "value": "2022-02-08T23:46:20.954882"
  },
  "last_modified": {
    "type": "/type/datetime",
    "value": "2025-11-16T13:37:30.465321"
  }
}`)

	node, err := ParseBookNode(payload)
	if err != nil {
		t.Fatalf("ParseBookNode error: %v", err)
	}
	if node.Key != "/books/OL37044446M" {
		t.Fatalf("unexpected key: %s", node.Key)
	}
	if len(node.Authors) != 1 || node.Authors[0] != "/authors/OL13066A" {
		t.Fatalf("unexpected authors: %+v", node.Authors)
	}
	if len(node.Works) != 1 || node.Works[0] != "/works/OL52267W" {
		t.Fatalf("unexpected works: %+v", node.Works)
	}
	if len(node.Subjects) != 1 || node.Subjects[0] != "Time travel--Fiction" {
		t.Fatalf("unexpected subjects: %+v", node.Subjects)
	}
}

func TestParseWorkNode(t *testing.T) {
	payload := []byte(`{
  "description": "Anthem is a tale of a future dark age of the great “we” – a world that deprives individuals of name, independence, and values.\n\nHe lived in the dark ages of the future. In a loveless world he dared to love the woman of his choice. In an age that had lost all traces of science and civilization he had the courage to seek and find knowledge. But these were not the crimes for which he would be hunted He was marked for death because he had committed the unpardonable sin: He had stood forth from the mindless human herd. He was alone.",
  "title": "Anthem",
  "covers": [
    802982,
    297072,
    6963895,
    361844,
    8318209,
    8740538,
    8740715,
    8741279,
    8741436,
    8741476,
    8741863,
    8742395,
    8742494,
    8788477,
    11269006
  ],
  "subjects": [
    "Fiction",
    "Individuality",
    "Time travel in fiction",
    "Individuality in fiction",
    "collectivism",
    "Time travel",
    "Psychology",
    "Men",
    "Men in fiction",
    "Man-woman relationships",
    "Man-woman relationships in fiction",
    "American fiction (fictional works by one author)",
    "Fiction, psychological",
    "Fiction, science fiction, general",
    "Fiction, dystopian",
    "Fiction, historical, general",
    "Individualism",
    "FICTION / Classics",
    "FICTION / Literary",
    "FICTION / Political",
    "Russian Science fiction",
    "Psychological fiction",
    "Unemployment insurance",
    "Taxation",
    "Law and legislation",
    "Local finance"
  ],
  "subject_people": [
    "Equality 7-2521",
    "Union 5-3992",
    "International 4-8818",
    "Liberty 5-3000"
  ],
  "key": "/works/OL731737W",
  "authors": [
    {
      "author": {
        "key": "/authors/OL59188A"
      },
      "type": {
        "key": "/type/author_role"
      }
    }
  ],
  "excerpts": [
    {
      "excerpt": "It is a sin to write this.",
      "comment": "first sentence",
      "author": {
        "key": "/people/kylalala"
      }
    }
  ],
  "type": {
    "key": "/type/work"
  },
  "latest_revision": 25,
  "revision": 25,
  "created": {
    "type": "/type/datetime",
    "value": "2009-12-09T02:38:46.377873"
  },
  "last_modified": {
    "type": "/type/datetime",
    "value": "2025-12-19T02:14:21.955641"
  }
}`)

	node, err := ParseWorkNode(payload)
	if err != nil {
		t.Fatalf("ParseWorkNode error: %v", err)
	}
	if node.Key != "/works/OL731737W" {
		t.Fatalf("unexpected key: %s", node.Key)
	}
	if len(node.Authors) != 1 || node.Authors[0] != "/authors/OL59188A" {
		t.Fatalf("unexpected authors: %+v", node.Authors)
	}
	if node.Description != "Anthem is a tale of a future dark age of the great “we” – a world that deprives individuals of name, independence, and values.\n\nHe lived in the dark ages of the future. In a loveless world he dared to love the woman of his choice. In an age that had lost all traces of science and civilization he had the courage to seek and find knowledge. But these were not the crimes for which he would be hunted He was marked for death because he had committed the unpardonable sin: He had stood forth from the mindless human herd. He was alone." {
		t.Fatalf("unexpected description: %s", node.Description)
	}
}

func TestParseAuthorNode(t *testing.T) {
	payload := []byte(`{
  "photos": [
    8060477,
    6289673
  ],
  "bio": "Jules Verne was a French author who helped pioneer the science-fiction genre.\n\nHe is best known for his novels A Journey to the Centre of the Earth (1864), From the Earth to the Moon (1865), Twenty Thousand Leagues Under the Sea (1869–1870), Around the World in Eighty Days (1873) and The Mysterious Island (1875). Verne wrote about space, air, and underwater travel before navigable aircraft and practical submarines were invented, and before any means of space travel had been devised. Consequently he is often referred to as the \"Father of science fiction\", along with H. G. Wells. ([Source][1].)\n\nAlso as of 2023, Jules Verne is regarded as the second most translated author in the world. ([Source][2].)\n\n  [1]: http://en.wikipedia.org/wiki/Jules_Verne\n  [2]: https://www.tomedes.com/translator-hub/most-translated-author.php",
  "alternate_names": [
    "Julio Verne",
    "Jül Vern",
    "Iulius Verne",
    "Júlio Verne",
    "Jules Vernes",
    "Jules Gabriel Verne",
    "(Fa) Fan, Erna",
    "Fo Er Nuo",
    "jules verne",
    "Jules Jules Verne",
    "Jules JULES VERNE",
    "Jules Gabriël Verne",
    "Жюль Верн",
    "Verne, Jules (edited By Charle F. Horne, Ph.D.)",
    "Jules (edited By Charles F. Horne) Verne",
    "Verne, Jules (edited By Charles F. Horne, Ph.D.",
    "M. Jules Verne",
    "(France)Jules Verne",
    "Verne Jules",
    "JULES VERNE",
    "Verne, Jules,",
    "Jules VERNE",
    "Verne,Jules",
    "Jules Verne -",
    "Jules \"Verne \"",
    "Jules Verne Verne",
    "Jules 1828-1905 Verne",
    "Jules Vern",
    "(fa) Fan, er na",
    "Julio VERNE",
    "JULIO VERNE",
    "julio verne",
    "Julio Julio  Verne",
    "VERNE JULIO",
    "Julio Verne Edebé (obra colectiva)",
    "Verne Julio"
  ],
  "death_date": "24 March 1905",
  "name": "Jules Verne",
  "remote_ids": {
    "project_gutenberg": "60",
    "amazon": "B000AQ6LZW",
    "librarything": "vernejules",
    "wikidata": "Q33977",
    "librivox": "189",
    "isni": "0000000121400562",
    "viaf": "76323989",
    "goodreads": "696805"
  },
  "source_records": [
    "bwb:9781545583470",
    "bwb:9781717444950"
  ],
  "date": "1828~1905",
  "title": "Verne, Jules",
  "personal_name": "Jules Verne",
  "key": "/authors/OL113611A",
  "birth_date": "8 February 1828",
  "type": {
    "key": "/type/author"
  },
  "links": [
    {
      "title": "Wikipedia entry (includes useful bibliography)",
      "url": "http://en.wikipedia.org/wiki/Jules_Verne",
      "type": {
        "key": "/type/link"
      }
    },
    {
      "title": "Le musée Jules Verne de Nantes",
      "url": "http://www.nantes.fr/julesverne/acc_5.htm",
      "type": {
        "key": "/type/link"
      }
    }
  ],
  "latest_revision": 43,
  "revision": 43,
  "created": {
    "type": "/type/datetime",
    "value": "2008-04-01T03:28:50.625462"
  },
  "last_modified": {
    "type": "/type/datetime",
    "value": "2025-10-04T16:03:45.611631"
  }
}`)

	node, err := ParseAuthorNode(payload)
	if err != nil {
		t.Fatalf("ParseAuthorNode error: %v", err)
	}
	if node.Key != "/authors/OL113611A" {
		t.Fatalf("unexpected key: %s", node.Key)
	}
	if node.Bio != "Jules Verne was a French author who helped pioneer the science-fiction genre.\n\nHe is best known for his novels A Journey to the Centre of the Earth (1864), From the Earth to the Moon (1865), Twenty Thousand Leagues Under the Sea (1869–1870), Around the World in Eighty Days (1873) and The Mysterious Island (1875). Verne wrote about space, air, and underwater travel before navigable aircraft and practical submarines were invented, and before any means of space travel had been devised. Consequently he is often referred to as the \"Father of science fiction\", along with H. G. Wells. ([Source][1].)\n\nAlso as of 2023, Jules Verne is regarded as the second most translated author in the world. ([Source][2].)\n\n  [1]: http://en.wikipedia.org/wiki/Jules_Verne\n  [2]: https://www.tomedes.com/translator-hub/most-translated-author.php" {
		t.Fatalf("unexpected bio: %s", node.Bio)
	}
	if len(node.Links) != 2 || node.Links[0].Title != "Wikipedia entry (includes useful bibliography)" {
		t.Fatalf("unexpected links: %+v", node.Links)
	}
}

func TestParseNodeDispatch(t *testing.T) {
	payload := []byte(`{"type":{"key":"/type/edition"},"key":"/books/OL1M","title":"Test"}`)
	node, err := ParseNode(payload)
	if err != nil {
		t.Fatalf("ParseNode error: %v", err)
	}
	if node == nil || node.NodeType() != models.NodeTypeEdition {
		t.Fatalf("unexpected node type: %#v", node)
	}
}

func TestParseSearchResponse(t *testing.T) {
	payload := []byte(`{
  "numFound": 6448,
  "start": 0,
  "numFoundExact": true,
  "num_found": 6448,
  "documentation_url": "https://openlibrary.org/dev/docs/api/search",
  "q": "",
  "offset": null,
  "docs": [
    {
      "author_key": [
        "OL59188A"
      ],
      "author_name": [
        "Ayn Rand"
      ],
      "cover_edition_key": "OL8663940M",
      "cover_i": 802982,
      "ebook_access": "borrowable",
      "edition_count": 103,
      "first_publish_year": 1936,
      "has_fulltext": true,
      "ia": [
        "anthem0000rand_t1s7",
        "anthem0000rand_z6v1",
        "anthem0000rand",
        "anthem00aynr",
        "anthemrand00rand",
        "anthem0000rand_w1o7",
        "anthem00aynr_0",
        "anthem00rand_0",
        "anthem05rand",
        "anthem00rand"
      ],
      "ia_collection": [
        "americana",
        "americanuniversity-ol"
      ],
      "key": "/works/OL731737W",
      "language": [
        "eng",
        "dut"
      ],
      "lending_edition_s": "OL32974194M",
      "lending_identifier_s": "anthem0000rand_t1s7",
      "public_scan_b": false,
      "title": "Anthem"
    },
    {
      "author_key": [
        "OL13066A"
      ],
      "author_name": [
        "H. G. Wells"
      ],
      "cover_edition_key": "OL27553880M",
      "cover_i": 9009316,
      "ebook_access": "public",
      "edition_count": 1154,
      "first_publish_year": 1895,
      "has_fulltext": true,
      "ia": [
        "timemachineinven00welluoft"
      ],
      "ia_collection": [
        "americana",
        "americanuniversity-ol",
        "bpljordan-ol"
      ],
      "key": "/works/OL52267W",
      "language": [
        "eng",
        "spa"
      ],
      "lending_edition_s": "OL24431723M",
      "lending_identifier_s": "timemachineinven00welluoft",
      "public_scan_b": true,
      "title": "The Time Machine",
      "id_standard_ebooks": [
        "h-g-wells/the-time-machine"
      ]
    }
  ]
}`)

	resp, err := ParseSearchResponse(payload)
	if err != nil {
		t.Fatalf("ParseSearchResponse error: %v", err)
	}
	if resp.NumFound != 6448 {
		t.Fatalf("unexpected numFound: %d", resp.NumFound)
	}
	if len(resp.Docs) != 2 || resp.Docs[0].Key != "/works/OL731737W" {
		t.Fatalf("unexpected docs: %+v", resp.Docs)
	}
}
