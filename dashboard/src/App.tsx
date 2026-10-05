import {
  BookOpen,
  ChevronRight,
  CircleArrowUp,
  CircleCheck,
  FlaskConical,
  Github,
  LayoutDashboard,
  Layers3,
  Menu,
  RefreshCw,
  SlidersHorizontal,
  X,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { BrandMark, ExternalLinkButton, Status } from "./components";
import { Configuration } from "./Configuration";
import { demoSnapshot, docsUrl } from "./data";
import { Overview } from "./Overview";
import type { Page } from "./types";
import { Upgrade } from "./Upgrade";

const pages = [
  {
    id: "overview",
    label: "Overview",
    icon: LayoutDashboard,
    description: "Your cluster, at a glance.",
  },
  {
    id: "configuration",
    label: "Configuration",
    icon: SlidersHorizontal,
    description: "The settings that make this cluster yours.",
  },
  {
    id: "upgrade",
    label: "Upgrade",
    icon: CircleArrowUp,
    description: "Review versions and plan your next upgrade.",
  },
] as const;

function currentPage(): Page {
  const route = window.location.hash.replace(/^#\/?/, "");
  return pages.find((page) => page.id === route)?.id ?? "overview";
}

export function App() {
  const [page, setPage] = useState<Page>(currentPage);
  const [menuOpen, setMenuOpen] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [lastRefresh, setLastRefresh] = useState<string | null>(null);
  const [refreshMessage, setRefreshMessage] = useState("");
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );
  const heading = useRef<HTMLHeadingElement>(null);
  const menuButton = useRef<HTMLButtonElement>(null);
  const navRef = useRef<HTMLElement>(null);
  const isFirstPage = useRef(true);
  const details = pages.find((item) => item.id === page)!;

  useEffect(() => {
    const onHashChange = () => {
      setPage(currentPage());
      setMenuOpen(false);
    };
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);

  useEffect(() => {
    document.title = `${details.label} · AK3S`;
    if (isFirstPage.current) {
      isFirstPage.current = false;
      return;
    }
    heading.current?.focus({ preventScroll: true });
    window.scrollTo({ top: 0, behavior: "instant" });
  }, [page, details.label]);

  useEffect(() => () => clearTimeout(refreshTimer.current), []);

  useEffect(() => {
    if (!menuOpen) return;
    navRef.current
      ?.querySelector<HTMLElement>('[aria-current="page"]')
      ?.focus();
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setMenuOpen(false);
        menuButton.current?.focus();
      }
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [menuOpen]);

  function refresh() {
    setRefreshing(true);
    setRefreshMessage("Refreshing sample data.");
    refreshTimer.current = setTimeout(() => {
      setLastRefresh(
        new Date().toLocaleTimeString([], {
          hour: "2-digit",
          minute: "2-digit",
        }),
      );
      setRefreshing(false);
      setRefreshMessage("Sample data refreshed. No live cluster is connected.");
    }, 500);
  }

  return (
    <div className="app-shell">
      <a
        className="skip-link"
        href="#main-content"
        onClick={(event) => {
          event.preventDefault();
          heading.current?.focus();
        }}
      >
        Skip to content
      </a>
      <aside className={`sidebar${menuOpen ? " sidebar-open" : ""}`}>
        <a
          href="#/overview"
          className="brand"
          aria-label="AK3S overview"
          onClick={() => setMenuOpen(false)}
        >
          <BrandMark />
          <span>
            AK3S<span className="brand-caption">A little Kubernetes.</span>
          </span>
        </a>
        <div className="sidebar-cluster">
          <span className="cluster-symbol">
            <Layers3 size={18} />
          </span>
          <div>
            <span className="sidebar-cluster-name">
              {demoSnapshot.cluster.name}
            </span>
            <span className="sidebar-cluster-caption">Demo cluster</span>
          </div>
          <span className="sidebar-cluster-dot" />
        </div>
        <div className="nav-label">WORKSPACE</div>
        <nav
          className="main-nav"
          id="main-navigation"
          aria-label="Main navigation"
          ref={navRef}
        >
          {pages.map((item) => (
            <a
              href={`#/${item.id}`}
              key={item.id}
              aria-current={page === item.id ? "page" : undefined}
              onClick={() => setMenuOpen(false)}
            >
              <item.icon size={18} strokeWidth={1.7} />
              <span>{item.label}</span>
              {item.id === "upgrade" && (
                <span
                  className="nav-update-dot"
                  aria-label="Update available"
                />
              )}
            </a>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <div className="sidebar-note">
            <span className="sidebar-note-icon">
              <Layers3 size={19} />
            </span>
            <p>
              Less to manage.
              <br />
              <strong>More to build.</strong>
            </p>
          </div>
          <ExternalLinkButton
            href={`${docsUrl}/operations.md`}
            className="sidebar-link"
          >
            <BookOpen size={16} />
            <span>Documentation</span>
          </ExternalLinkButton>
          <ExternalLinkButton
            href="https://github.com/jgador/ak3s"
            className="sidebar-link"
          >
            <Github size={16} />
            <span>GitHub</span>
          </ExternalLinkButton>
          <div className="sidebar-version">
            <span>
              AK3S{" "}
              <span className="mono">{demoSnapshot.cluster.ak3sVersion}</span>
            </span>
            <span className="sidebar-version-dot" />
          </div>
        </div>
      </aside>
      <div className="workspace">
        <header className="topbar">
          <button
            className="icon-button mobile-menu"
            ref={menuButton}
            aria-label={menuOpen ? "Close navigation" : "Open navigation"}
            aria-expanded={menuOpen}
            aria-controls="main-navigation"
            onClick={() => setMenuOpen(!menuOpen)}
          >
            {menuOpen ? <X size={20} /> : <Menu size={20} />}
          </button>
          <nav className="breadcrumb" aria-label="Breadcrumb">
            <Layers3 size={16} />
            <span>Clusters</span>
            <ChevronRight size={12} />
            <a href="#/overview">{demoSnapshot.cluster.name}</a>
            <ChevronRight size={12} />
            <span aria-current="page">{details.label}</span>
          </nav>
          <span
            className="demo-badge"
            title="All cluster values are sample data. No live cluster is connected."
          >
            <FlaskConical size={13} />
            Demo mode
          </span>
        </header>
        <main id="main-content" className="main-content">
          <div className="page-heading">
            <div>
              <div className="eyebrow">CLUSTER CONTROL PLANE</div>
              <h1 ref={heading} tabIndex={-1}>
                {details.label}
              </h1>
              <p>{details.description}</p>
            </div>
            <div className="page-heading-actions">
              {page === "overview" ? (
                <>
                  <div className="operational">
                    <CircleCheck size={15} />
                    <span>All systems operational</span>
                  </div>
                  <div className="refresh-control">
                    <span>
                      {lastRefresh
                        ? `Sample refreshed at ${lastRefresh}`
                        : "Sample snapshot"}
                    </span>
                    <button
                      className="icon-button"
                      onClick={refresh}
                      disabled={refreshing}
                      aria-label="Refresh sample data"
                      title="Refresh sample data"
                    >
                      <RefreshCw
                        className={refreshing ? "spinning" : ""}
                        size={15}
                      />
                    </button>
                  </div>
                </>
              ) : (
                <Status
                  health={demoSnapshot.cluster.health}
                  label="Cluster healthy"
                />
              )}
            </div>
          </div>
          {page === "overview" && <Overview snapshot={demoSnapshot} />}
          {page === "configuration" && (
            <Configuration snapshot={demoSnapshot} />
          )}
          {page === "upgrade" && <Upgrade snapshot={demoSnapshot} />}
          <div className="sr-only" role="status">
            {refreshMessage}
          </div>
        </main>
      </div>
    </div>
  );
}
