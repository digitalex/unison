import type { ThemeDocument } from './lib/theme-model';

// Exact current project snapshot. Keep the internal theme identity for preview behavior.
export const theme: ThemeDocument = {
  "schemaVersion": 2,
  "name": "GraphicalUIKomore",
  "theme": {
    "name": "Grove",
    "text": {
      "l": {
        "size": 23.697,
        "lineHeight": 31.596
      },
      "m": {
        "size": 15.798,
        "lineHeight": 23.697
      },
      "s": {
        "size": 13.823,
        "lineHeight": 19.748
      },
      "xl": {
        "size": 35.546,
        "lineHeight": 39.495
      },
      "xs": {
        "size": 11.848,
        "lineHeight": 15.798
      },
      "xxl": {
        "size": 47.394,
        "lineHeight": 51.343
      },
      "xxs": {
        "size": 9.874,
        "lineHeight": 13.823
      }
    },
    "fonts": {
      "ui": {
        "family": "\"Sidebar Geist\", sans-serif",
        "weights": {
          "heavy": 600,
          "medium": 400,
          "regular": 300
        }
      },
      "data": {
        "family": "\"Sidebar Geist Mono\", monospace",
        "weights": {
          "heavy": 600,
          "medium": 400,
          "regular": 300
        }
      },
      "brand": {
        "family": "\"Sidebar Geist\", sans-serif",
        "weights": {
          "heavy": 600,
          "medium": 400,
          "regular": 300
        }
      },
      "editorial": {
        "family": "\"Sidebar Geist\", sans-serif",
        "weights": {
          "heavy": 600,
          "medium": 400,
          "regular": 300
        }
      }
    },
    "border": {
      "l": 2,
      "m": 1,
      "s": 1,
      "none": 0
    },
    "colors": {
      "dark": {
        "error": "#ff5263",
        "color-1": "#e5e5e5",
        "color-2": "#b1b1b1",
        "color-3": "#777777",
        "color-4": "#424242",
        "success": "#76ef6b",
        "warning": "#ffa344",
        "neutral-1": "#000000",
        "neutral-2": "#21201d",
        "neutral-3": "#2c2b28",
        "neutral-4": "#393835",
        "neutral-5": "#52504c",
        "neutral-6": "#797773",
        "neutral-7": "#a5a39f",
        "neutral-8": "#c8c6c2",
        "neutral-9": "#e6e5e3",
        "neutral-10": "#ffffff"
      },
      "light": {
        "error": "#ff5263",
        "color-1": "#262626",
        "color-2": "#707070",
        "color-3": "#b6b6b6",
        "color-4": "#e8e8e8",
        "success": "#76ef6b",
        "warning": "#ffa344",
        "neutral-1": "#ffffff",
        "neutral-2": "#fafaf9",
        "neutral-3": "#f2f0ec",
        "neutral-4": "#e1ddd5",
        "neutral-5": "#cbc5b9",
        "neutral-6": "#9b968a",
        "neutral-7": "#716c61",
        "neutral-8": "#4a453b",
        "neutral-9": "#29241b",
        "neutral-10": "#000000"
      }
    },
    "radius": {
      "l": 22.13,
      "m": 11.33,
      "s": 7.54,
      "xl": 33.26,
      "xs": 3.75,
      "full": 9999,
      "zero": 0
    },
    "shadows": {
      "l": {
        "x": 0,
        "y": 12,
        "blur": 30,
        "color": {
          "dark": "neutral-1",
          "light": "neutral-10"
        },
        "spread": 0,
        "opacity": 0
      },
      "m": {
        "x": 0,
        "y": 8,
        "blur": 24,
        "color": {
          "dark": "neutral-1",
          "light": "neutral-10"
        },
        "spread": 0,
        "opacity": 0
      },
      "s": {
        "x": 0,
        "y": 1,
        "blur": 2,
        "color": {
          "dark": "neutral-1",
          "light": "neutral-10"
        },
        "spread": 0,
        "opacity": 0
      }
    },
    "spacing": {
      "l": 23.199,
      "m": 15.466,
      "s": 11.599,
      "xl": 30.933,
      "xs": 7.733,
      "xxl": 46.399,
      "xxs": 3.866,
      "zero": 0
    },
    "animation": {
      "type": "easing",
      "large": {
        "type": "easing",
        "bounce": 0.3,
        "easing": [
          0.16,
          1,
          0.3,
          1
        ],
        "duration": 280,
        "visualDuration": 0.36
      },
      "bounce": 0.2,
      "easing": [
        0.16,
        1,
        0.3,
        1
      ],
      "duration": 160,
      "popupScale": 0.96,
      "pressDistance": 1,
      "visualDuration": 0.2
    },
    "iconStyle": "outlined",
    "iconFamily": "Lucide",
    "neutralTone": "warm",
    "colorEmphasis": 19.1943359375,
    "letterSpacing": {
      "l": 0.01,
      "m": 0,
      "s": -0.01,
      "xl": 0.02,
      "xs": -0.02
    },
    "primaryForeground": {
      "dark": "neutral-2",
      "light": "neutral-2"
    },
    "primaryActionColor": "neutral-10",
    "colorPalettes": [
      {
        "id": "grove-signal",
        "name": "Signal",
        "description": "Bright green with cyan, electric yellow, and violet accents.",
        "colors": {
          "light": [
            "#76ef6b",
            "#00c9bb",
            "#E0E709",
            "#e864ff"
          ],
          "dark": [
            "#76ef6b",
            "#00c9bb",
            "#f4f83b",
            "#e864ff"
          ]
        },
        "primaryForeground": {
          "light": "neutral-9",
          "dark": "neutral-2"
        }
      },
      {
        "id": "grove-electric",
        "name": "Electric",
        "description": "Electric yellow with green, blue, and violet accents.",
        "colors": {
          "light": [
            "#E0E709",
            "#76ef6b",
            "#009ff0",
            "#e864ff"
          ],
          "dark": [
            "#f4f83b",
            "#76ef6b",
            "#009ff0",
            "#e864ff"
          ]
        },
        "primaryForeground": {
          "light": "neutral-9",
          "dark": "neutral-2"
        }
      },
      {
        "id": "grove-cyan",
        "name": "Cyan",
        "description": "Clear cyan with blue, green, and violet accents.",
        "colors": {
          "light": [
            "#00c9bb",
            "#009ff0",
            "#76ef6b",
            "#e864ff"
          ],
          "dark": [
            "#00c9bb",
            "#009ff0",
            "#76ef6b",
            "#e864ff"
          ]
        },
        "primaryForeground": {
          "light": "neutral-9",
          "dark": "neutral-2"
        }
      },
      {
        "id": "grove-blue",
        "name": "Blue",
        "description": "Vivid blue with cyan, green, and violet accents.",
        "colors": {
          "light": [
            "#009ff0",
            "#00c9bb",
            "#76ef6b",
            "#e864ff"
          ],
          "dark": [
            "#009ff0",
            "#00c9bb",
            "#76ef6b",
            "#e864ff"
          ]
        },
        "primaryForeground": {
          "light": "neutral-9",
          "dark": "neutral-2"
        }
      },
      {
        "id": "grove-violet",
        "name": "Violet",
        "description": "Bright violet with cyan, green, and blue accents.",
        "colors": {
          "light": [
            "#e864ff",
            "#00c9bb",
            "#76ef6b",
            "#009ff0"
          ],
          "dark": [
            "#e864ff",
            "#00c9bb",
            "#76ef6b",
            "#009ff0"
          ]
        },
        "primaryForeground": {
          "light": "neutral-9",
          "dark": "neutral-2"
        }
      },
      {
        "id": "grove-monochrome",
        "name": "Monochrome",
        "description": "Charcoal and clear grays for a neutral technical interface.",
        "colors": {
          "light": [
            "#262626",
            "#707070",
            "#b6b6b6",
            "#e8e8e8"
          ],
          "dark": [
            "#e5e5e5",
            "#b1b1b1",
            "#777777",
            "#424242"
          ]
        },
        "primaryForeground": {
          "light": "neutral-2",
          "dark": "neutral-2"
        }
      },
      {
        "id": "grove-orange",
        "name": "Orange",
        "description": "Bright orange with cyan, green, and violet accents.",
        "colors": {
          "light": [
            "#ffa344",
            "#00c9bb",
            "#76ef6b",
            "#e864ff"
          ],
          "dark": [
            "#ffa344",
            "#00c9bb",
            "#76ef6b",
            "#e864ff"
          ]
        },
        "primaryForeground": {
          "light": "neutral-9",
          "dark": "neutral-2"
        }
      }
    ],
    "paletteBaseNeutrals": {
      "light": {
        "neutral-1": "#ffffff",
        "neutral-2": "#fafafa",
        "neutral-3": "#f0f0f0",
        "neutral-4": "#dedede",
        "neutral-5": "#c5c5c5",
        "neutral-6": "#969696",
        "neutral-7": "#6c6c6c",
        "neutral-8": "#454545",
        "neutral-9": "#252525",
        "neutral-10": "#000000"
      },
      "dark": {
        "neutral-1": "#000000",
        "neutral-2": "#202020",
        "neutral-3": "#2b2b2b",
        "neutral-4": "#383838",
        "neutral-5": "#505050",
        "neutral-6": "#777777",
        "neutral-7": "#a3a3a3",
        "neutral-8": "#c6c6c6",
        "neutral-9": "#e5e5e5",
        "neutral-10": "#ffffff"
      }
    },
    "keyColorPalette": [
      "#76ef6b",
      "#E0E709",
      "#00c9bb",
      "#009ff0",
      "#e864ff",
      "#262626",
      "#ffa344"
    ]
  },
  "componentTokens": {
    "switch:default:part:row:rest": {
      "gap": "s"
    },
    "switch:default:part:label:rest": {
      "textSize": "m"
    },
    "checkbox:default:part:label:rest": {
      "textSize": "m"
    },
    "switch:default:part:control:rest": {
      "controlSize": "xl"
    }
  },
  "componentVariants": {}
};
