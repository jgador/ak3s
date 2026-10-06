import {
  ArrowRight,
  FileCode2,
  GitMerge,
  Info,
  LockKeyhole,
  SlidersHorizontal,
} from "lucide-react";
import { useState } from "react";
import { CodeBlock, CopyButton, ExternalLinkButton } from "./components";
import { docsUrl } from "./data";
import type { DashboardSnapshot } from "./types";

export function Configuration({ snapshot }: { snapshot: DashboardSnapshot }) {
  const [view, setView] = useState<"effective" | "overrides">("effective");
  const config = snapshot.configuration;
  const yaml =
    view === "effective" ? config.effectiveYaml : config.overridesYaml;

  return (
    <>
      <div className="config-summary">
        <div className="config-summary-icon">
          <GitMerge size={22} />
        </div>
        <div>
          <h2>Your configuration, resolved.</h2>
          <p>AK3S defaults + {config.overrideCount} operator overrides</p>
        </div>
        <span className="read-only">
          <LockKeyhole size={13} />
          Read only
        </span>
      </div>
      <div className="configuration-layout">
        <section className="panel config-panel" aria-label="AK3S configuration">
          <div className="config-toolbar">
            <div className="segmented-control" aria-label="Configuration view">
              <button
                aria-pressed={view === "effective"}
                onClick={() => setView("effective")}
              >
                Effective configuration
              </button>
              <button
                aria-pressed={view === "overrides"}
                onClick={() => setView("overrides")}
              >
                Overrides
                <span className="tab-count">{config.overrideCount}</span>
              </button>
            </div>
            <CopyButton text={yaml} label="Copy YAML" />
          </div>
          <div className="file-heading">
            <FileCode2 size={15} />
            <code>
              {view === "effective"
                ? "Effective AK3S configuration"
                : config.path}
            </code>
            <span>YAML</span>
          </div>
          <CodeBlock code={yaml} numbered />
          <div className="panel-footer">
            <Info size={13} />
            <span>
              {view === "effective"
                ? "Defaults are merged with operator overrides."
                : "Settings from the operator configuration file are shown."}
            </span>
          </div>
        </section>
        <aside className="configuration-aside">
          <section className="panel guidance-panel">
            <span className="guidance-icon">
              <SlidersHorizontal size={20} />
            </span>
            <h2>Keep your changes small.</h2>
            <p>
              Set only the values you need to change. AK3S supplies the rest
              from its embedded defaults.
            </p>
            <div className="guidance-divider" />
            <h3>Operator overrides</h3>
            <code className="path-label">{config.path}</code>
            <p>
              Edit this file on the cluster host, then preview and apply your
              changes with the CLI.
            </p>
            <CodeBlock code={"sudo ak3s apply --dry-run\nsudo ak3s apply"} />
            <ExternalLinkButton
              className="text-link"
              href={`${docsUrl}/configuration.md`}
            >
              Configuration reference
            </ExternalLinkButton>
          </section>
          <div className="small-note">
            <Info size={16} />
            <p>
              Mappings merge; lists replace defaults. Omit a key to inherit its
              default value.
            </p>
          </div>
          <a className="back-link" href="#/overview">
            Back to overview
            <ArrowRight size={14} />
          </a>
        </aside>
      </div>
    </>
  );
}
