// spaNavigate moves to a same-origin path without reloading: App listens
// for popstate and renders the route. For links outside App's props.
export function spaNavigate(to) {
  window.history.pushState(null, "", to);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

// spaLink returns props for an <a> that navigates within the app.
export function spaLink(to, onNavigate) {
  return {
    href: to,
    onClick: (e) => {
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.button === 1) return;
      e.preventDefault();
      onNavigate?.();
      spaNavigate(to);
    },
  };
}
