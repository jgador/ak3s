import {
  BookOpen,
  ChevronRight,
  CircleArrowUp,
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
import { docsUrl } from "./data";
import { Overview } from "./Overview";
import type { DashboardSnapshot, Page } from "./types";
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
  const [snapshot, setSnapshot] = useState<DashboardSnapshot | null>(null);
  const [error, setError] = useState("");
  const request = useRef<AbortController | null>(null);
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

  useEffect(() => {
    void refresh();
    const timer = setInterval(() => void refresh(), 30_000);
    return () => {
      clearInterval(timer);
      request.current?.abort();
    };
  }, []);

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

  async function refresh() {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setRefreshing(true);
    try {
      const response = await fetch("/api/snapshot", {
        cache: "no-store",
        signal: controller.signal,
      });
      if (!response.ok) {
        throw new Error(
          "Cannot read cluster data. Check that K3s and the dashboard service account are available.",
        );
      }
      const data: DashboardSnapshot = await response.json();
      if (controller.signal.aborted) return;
      setSnapshot(data);
      setLastRefresh(new Date(data.collectedAt).toLocaleTimeString());
      setError("");
      setRefreshMessage("Live cluster data updated.");
    } catch (failure) {
      if (controller.signal.aborted) return;
      setError(
        failure instanceof Error ? failure.message : "Cannot read the local cluster.",
      );
      setRefreshMessage("Cluster refresh failed.");
    } finally {
      if (!controller.signal.aborted) setRefreshing(false);
    }
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
              {snapshot?.cluster.name?.trim() || "Not available"}
            </span>
            <span className="sidebar-cluster-caption">
              {snapshot ? (error ? "Connection lost" : "Connected cluster") : "Not connected"}
            </span>
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
              {item.id === "upgrade" && snapshot?.upgrade.availableVersion && (
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
              <span className="mono">
                {snapshot?.cluster.ak3sVersion || "Not available"}
              </span>
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
            <a href="#/overview">
              {snapshot?.cluster.name?.trim() || "Not available"}
            </a>
            <ChevronRight size={12} />
            <span aria-current="page">{details.label}</span>
          </nav>
          <span className="demo-badge">
            {snapshot ? (error ? "Stale data" : "Live cluster") : "Not connected"}
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
                  <Status
                    health={error ? "unknown" : snapshot?.cluster.health ?? "unknown"}
                  />
                  <div className="refresh-control">
                    <span>
                      {lastRefresh
                        ? `Updated at ${lastRefresh}`
                        : "Waiting for cluster data"}
                    </span>
                    <button
                      className="icon-button"
                      onClick={refresh}
                      disabled={refreshing}
                      aria-label="Refresh cluster data"
                      title="Refresh cluster data"
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
                  health={error ? "unknown" : snapshot?.cluster.health ?? "unknown"}
                />
              )}
            </div>
          </div>
          {error && (
            <div className="panel connection-error" role="alert">
              {error}{snapshot && " Showing the last successful snapshot."}
            </div>
          )}
          {!snapshot && (
            <div className="panel connection-error">
              {refreshing ? "Reading the local K3s cluster…" : "No cluster data available."}
            </div>
          )}
          {snapshot && page === "overview" && <Overview snapshot={snapshot} />}
          {snapshot && page === "configuration" && (
            <Configuration snapshot={snapshot} />
          )}
          {snapshot && page === "upgrade" && <Upgrade snapshot={snapshot} />}
          <div className="sr-only" role="status">
            {refreshMessage}
          </div>
        </main>
      </div>
    </div>
  );
}
