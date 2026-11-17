// ScreenHead is the header of a workspace screen (backlog, chat): its
// title, optional controls (tools) and note beside it, then the screen's
// actions (children). On phones it becomes a large, flat title.
export default function ScreenHead({ title, tools, note, children }) {
  return (
    <header className="screen-head">
      <h2 className="screen-title">{title}</h2>
      {tools}
      {note && <span className="screen-note">{note}</span>}
      <span className="spacer" />
      {children}
    </header>
  );
}
