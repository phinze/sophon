interface Route {
  pattern: RegExp;
  paramNames: string[];
  mount: (params: Record<string, string>) => void;
  unmount: () => void;
}

let routes: Route[] = [];
let currentUnmount: (() => void) | null = null;

export function add(
  path: string,
  mount: (params: Record<string, string>) => void,
  unmount: () => void,
): void {
  const paramNames: string[] = [];
  const pattern = new RegExp(
    "^" +
      path.replace(/:([^/]+)/g, (_, name) => {
        paramNames.push(name);
        return "([^/]+)";
      }) +
      "$",
  );
  routes.push({ pattern, paramNames, mount, unmount });
}

function resolve(pathname: string): void {
  if (currentUnmount) {
    currentUnmount();
    currentUnmount = null;
  }
  for (const route of routes) {
    const m = pathname.match(route.pattern);
    if (m) {
      const params: Record<string, string> = {};
      route.paramNames.forEach((name, i) => {
        params[name] = m[i + 1];
      });
      currentUnmount = route.unmount;
      route.mount(params);
      return;
    }
  }
}

export function navigate(path: string): void {
  history.pushState(null, "", path);
  resolve(path);
}

export function start(): void {
  window.addEventListener("popstate", () => {
    resolve(window.location.pathname);
  });

  // Intercept link clicks for SPA navigation
  document.addEventListener("click", (e) => {
    const anchor = (e.target as HTMLElement).closest("a");
    if (!anchor) return;
    const href = anchor.getAttribute("href");
    if (!href || href.startsWith("http") || href.startsWith("//")) return;
    e.preventDefault();
    navigate(href);
  });

  resolve(window.location.pathname);
}
