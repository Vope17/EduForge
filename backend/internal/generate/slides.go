package generate

import "encoding/json"

const slidePreviewInstruction = languageInstruction + ` Generate only one small slide style preview, never the full deck. Use exactly one heading and one or two short content elements, each at most 300 characters, with notes at most 300 characters. The outer layout and page layout must match. For image_text include an image placeholder with an empty image_url and a caption. For unused items use an empty array, and always use an empty image_url. Do not add IDs. Revise style according to feedback when present.`

const slidesInstruction = languageInstruction + ` Create classroom slides with exactly the requested slide_count. Each root block is a slide with exactly one heading and at least one content element. Every page must use confirmed_template.layout. Use concise explanations and useful teaching examples, and put longer explanations in speaker notes. For two_columns use two content elements. For image_text include an image placeholder with an empty image_url and a caption. Never invent image links. Use an empty items array when unused and empty image_url. Do not add IDs. Include an introduction, clear teaching points, and a concluding activity or recap, as appropriate to the topic and audience.`

var slidePreviewSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "language",
    "palette",
    "font",
    "layout",
    "tone",
    "density",
    "example"
  ],
  "properties": {
    "language": {
      "type": "string",
      "enum": [
        "zh-TW",
        "en"
      ]
    },
    "palette": {
      "type": "string",
      "enum": [
        "forest",
        "ocean",
        "warm"
      ]
    },
    "font": {
      "type": "string",
      "enum": [
        "sans",
        "serif"
      ]
    },
    "layout": {
      "type": "string",
      "enum": [
        "title_body",
        "two_columns",
        "image_text"
      ]
    },
    "tone": {
      "type": "string",
      "enum": [
        "friendly",
        "formal"
      ]
    },
    "density": {
      "type": "string",
      "enum": [
        "compact",
        "balanced",
        "detailed"
      ]
    },
    "example": {
      "type": "array",
      "minItems": 1,
      "maxItems": 1,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "kind",
          "text",
          "layout",
          "notes",
          "children"
        ],
        "properties": {
          "kind": {
            "type": "string",
            "enum": [
              "slide"
            ]
          },
          "text": {
            "type": "string",
            "enum": [
              ""
            ]
          },
          "layout": {
            "type": "string",
            "enum": [
              "title_body",
              "two_columns",
              "image_text"
            ]
          },
          "notes": {
            "type": "string",
            "description": "Speaker notes. At most 2500 characters."
          },
          "children": {
            "type": "array",
            "minItems": 2,
            "maxItems": 8,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": [
                "kind",
                "text",
                "items",
                "image_url"
              ],
              "properties": {
                "kind": {
                  "type": "string",
                  "enum": [
                    "heading",
                    "paragraph",
                    "list",
                    "image"
                  ]
                },
                "text": {
                  "type": "string",
                  "description": "Title at most 120 characters; body at most 800 characters. Use concise classroom text."
                },
                "items": {
                  "type": "array",
                  "maxItems": 8,
                  "items": {
                    "type": "string",
                    "description": "At most 200 characters."
                  }
                },
                "image_url": {
                  "type": "string",
                  "enum": [
                    ""
                  ]
                }
              }
            }
          }
        }
      }
    }
  }
}`)

var slidesContentSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "language",
    "title",
    "blocks"
  ],
  "properties": {
    "language": {
      "type": "string",
      "enum": [
        "zh-TW",
        "en"
      ]
    },
    "title": {
      "type": "string",
      "description": "At most 160 characters."
    },
    "blocks": {
      "type": "array",
      "minItems": 1,
      "maxItems": 12,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "kind",
          "text",
          "layout",
          "notes",
          "children"
        ],
        "properties": {
          "kind": {
            "type": "string",
            "enum": [
              "slide"
            ]
          },
          "text": {
            "type": "string",
            "enum": [
              ""
            ]
          },
          "layout": {
            "type": "string",
            "enum": [
              "title_body",
              "two_columns",
              "image_text"
            ]
          },
          "notes": {
            "type": "string",
            "description": "Speaker notes. At most 2500 characters."
          },
          "children": {
            "type": "array",
            "minItems": 2,
            "maxItems": 8,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": [
                "kind",
                "text",
                "items",
                "image_url"
              ],
              "properties": {
                "kind": {
                  "type": "string",
                  "enum": [
                    "heading",
                    "paragraph",
                    "list",
                    "image"
                  ]
                },
                "text": {
                  "type": "string",
                  "description": "Title at most 120 characters; body at most 800 characters. Use concise classroom text."
                },
                "items": {
                  "type": "array",
                  "maxItems": 8,
                  "items": {
                    "type": "string",
                    "description": "At most 200 characters."
                  }
                },
                "image_url": {
                  "type": "string",
                  "enum": [
                    ""
                  ]
                }
              }
            }
          }
        }
      }
    }
  }
}`)
