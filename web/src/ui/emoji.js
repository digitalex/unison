// The emoji offered by ui/EmojiPicker.jsx (#305): a short, categorized set
// for a participant's emoji, each with a label and the words a search
// finds it by. Anything else can still be typed or pasted into the field.
//
// The first group is the store's defaultEmojis (internal/store/store.go),
// in the same order: the mascots handed out to people who pick nothing.
const e = (emoji, label, keywords = "") => ({ emoji, label, keywords });

export const EMOJI_GROUPS = [
  {
    title: "Unison mascots",
    emoji: [
      e("🦊", "Fox", "mascot animal orange"),
      e("🐙", "Octopus", "mascot animal sea tentacle"),
      e("🦉", "Owl", "mascot animal bird wise night"),
      e("🐝", "Bee", "mascot animal insect honey busy"),
      e("🐢", "Turtle", "mascot animal slow shell tortoise"),
      e("🦄", "Unicorn", "mascot animal magic horse"),
      e("🐧", "Penguin", "mascot animal bird linux"),
      e("🦖", "T-Rex", "mascot animal dinosaur dino"),
      e("🐬", "Dolphin", "mascot animal sea"),
      e("🦜", "Parrot", "mascot animal bird"),
      e("🐳", "Whale", "mascot animal sea docker"),
      e("🦔", "Hedgehog", "mascot animal spiky"),
    ],
  },
  {
    title: "Animals & nature",
    emoji: [
      e("🐶", "Dog", "animal puppy pet"),
      e("🐱", "Cat", "animal kitten pet"),
      e("🐻", "Bear", "animal"),
      e("🐼", "Panda", "animal bear"),
      e("🐨", "Koala", "animal"),
      e("🦁", "Lion", "animal cat king"),
      e("🐯", "Tiger", "animal cat"),
      e("🐸", "Frog", "animal toad green"),
      e("🐵", "Monkey", "animal ape"),
      e("🐺", "Wolf", "animal dog"),
      e("🦝", "Raccoon", "animal"),
      e("🦦", "Otter", "animal river"),
      e("🦥", "Sloth", "animal slow"),
      e("🦩", "Flamingo", "animal bird pink"),
      e("🦅", "Eagle", "animal bird"),
      e("🦋", "Butterfly", "animal insect"),
      e("🐞", "Ladybug", "animal insect bug ladybird"),
      e("🐌", "Snail", "animal slow shell"),
      e("🌵", "Cactus", "nature plant desert"),
      e("🌲", "Evergreen tree", "nature plant pine forest juniper"),
      e("🌻", "Sunflower", "nature plant flower"),
      e("🍄", "Mushroom", "nature fungus"),
    ],
  },
  {
    title: "Faces & people",
    emoji: [
      e("😀", "Grinning face", "smile happy"),
      e("😎", "Face with sunglasses", "smile cool"),
      e("🤓", "Nerd face", "smile glasses geek"),
      e("🤠", "Cowboy", "face hat"),
      e("🥳", "Partying face", "celebrate party hat"),
      e("😇", "Smiling face with halo", "angel innocent"),
      e("🤖", "Robot", "face bot machine agent"),
      e("👻", "Ghost", "face spooky halloween"),
      e("👽", "Alien", "face space ufo"),
      e("👾", "Alien monster", "face game space invader arcade"),
      e("🧙", "Mage", "person wizard witch magic"),
      e("🧑‍🚀", "Astronaut", "person space"),
      e("🧑‍💻", "Technologist", "person developer coder programmer laptop"),
      e("🧑‍🎨", "Artist", "person designer painter"),
      e("🧑‍🔬", "Scientist", "person researcher lab"),
      e("🥷", "Ninja", "person stealth"),
      e("🦸", "Superhero", "person hero cape"),
      e("🧛", "Vampire", "person dracula halloween"),
    ],
  },
  {
    title: "Objects & symbols",
    emoji: [
      e("🚀", "Rocket", "object space launch ship"),
      e("⭐", "Star", "symbol favorite"),
      e("⚡", "Lightning", "symbol bolt fast zap electric"),
      e("🔥", "Fire", "symbol flame hot"),
      e("🌈", "Rainbow", "symbol color"),
      e("🌙", "Moon", "symbol night crescent"),
      e("☀️", "Sun", "symbol day sunny"),
      e("❄️", "Snowflake", "symbol cold winter ice"),
      e("💎", "Gem", "object diamond jewel"),
      e("🎯", "Bullseye", "object target dart goal"),
      e("🎲", "Die", "object dice game random"),
      e("🎸", "Guitar", "object music rock"),
      e("🎨", "Palette", "object art paint design"),
      e("🔧", "Wrench", "object tool fix spanner"),
      e("🔭", "Telescope", "object space astronomy"),
      e("🧭", "Compass", "object navigation direction"),
      e("🧩", "Puzzle piece", "object jigsaw"),
      e("💡", "Light bulb", "object idea"),
      e("📚", "Books", "object library read"),
      e("⚓", "Anchor", "object ship sea"),
    ],
  },
  {
    title: "Food & treats",
    emoji: [
      e("🍎", "Apple", "food fruit red"),
      e("🍋", "Lemon", "food fruit citrus"),
      e("🍉", "Watermelon", "food fruit"),
      e("🍓", "Strawberry", "food fruit berry"),
      e("🍑", "Peach", "food fruit"),
      e("🥑", "Avocado", "food fruit"),
      e("🌶️", "Hot pepper", "food chili spicy"),
      e("🥕", "Carrot", "food vegetable"),
      e("🍕", "Pizza", "food slice"),
      e("🌮", "Taco", "food mexican"),
      e("🍣", "Sushi", "food fish japanese"),
      e("🍪", "Cookie", "treat sweet biscuit"),
      e("🍩", "Doughnut", "treat sweet donut"),
      e("🧁", "Cupcake", "treat sweet cake"),
      e("🍰", "Cake", "treat sweet shortcake dessert"),
      e("☕", "Coffee", "drink hot cup tea"),
    ],
  },
];

// searchEmoji returns the groups narrowed to the emoji whose label or
// keywords hold every word of query (case does not matter; a pasted emoji
// finds itself); groups left empty are dropped. An empty query returns
// everything.
export function searchEmoji(query) {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return EMOJI_GROUPS;
  return EMOJI_GROUPS.map((group) => ({
    ...group,
    emoji: group.emoji.filter((item) => {
      const text = `${item.emoji} ${item.label} ${item.keywords}`.toLowerCase();
      return words.every((word) => text.includes(word));
    }),
  })).filter((group) => group.emoji.length > 0);
}
