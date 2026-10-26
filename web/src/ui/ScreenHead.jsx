// ScreenHead is the header of a workspace screen (inbox, backlog, chat): its
// title, an optional note beside it, then the screen's actions (children).
// On phones it becomes a large, flat title.
export default function ScreenHead({ title, note, children }) {
  return (
    <header className="screen-head">
      <h2 className="screen-title">{title}</h2>
      {note && <span className="screen-note">{note}</span>}
      <span className="spacer" />
      {children}
    </header>
  );
}
