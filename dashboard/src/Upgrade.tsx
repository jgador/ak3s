import {
  ArrowRight,
  ArrowUpRight,
  ArrowUpToLine,
  Box,
  Check,
  ChevronDown,
  CircleArrowUp,
  ClipboardList,
  Info,
  Terminal,
} from "lucide-react";
import { useState } from "react";
import { CodeBlock, CopyButton, ExternalLinkButton } from "./components";
import { docsUrl, releasesUrl, upgradeCommands } from "./data";
import type { DashboardSnapshot } from "./types";

export function Upgrade({ snapshot }: { snapshot: DashboardSnapshot }) {
  const [stepsOpen, setStepsOpen] = useState(false);
  return (
    <>
      <div className="upgrade-banner">
        <span className="upgrade-banner-icon">
          <CircleArrowUp size={24} />
        </span>
        <div>
          <h2>{snapshot.upgrade.availableVersion ? "A new version is available." : "Review cluster upgrades."}</h2>
          <p>Review the release and plan your next AK3S upgrade.</p>
        </div>
        <span className="badge">{snapshot.upgrade.availableVersion ? "Update available" : "Not checked"}</span>
      </div>
      <div className="release-comparison">
        <section className="panel release-card">
          <div className="release-label">
            <Box size={17} />
            <span>Last attempted AK3S release</span>
            <span className="badge">Current</span>
          </div>
          <h2>{snapshot.cluster.ak3sVersion}</h2>
          <p>
            AK3S on{" "}
            <strong>{snapshot.cluster.name?.trim() || "Not available"}</strong>
          </p>
          <div className="release-detail">
            <Check size={15} />
            <span>K3s {snapshot.cluster.k3sVersion}</span>
          </div>
        </section>
        <span className="release-arrow">
          <ArrowRight size={20} />
        </span>
        <section className="panel release-card release-available">
          <div className="release-label">
            <ArrowUpToLine size={17} />
            <span>Available version</span>
            <span className="badge">{snapshot.upgrade.availableVersion ? "Available" : "Not checked"}</span>
          </div>
          <h2>{snapshot.upgrade.availableVersion ?? "Not checked"}</h2>
          <p>Browse published releases before choosing an upgrade.</p>
          <div className="release-detail">
            <Info size={15} />
            <span>Review the release's pinned component versions.</span>
          </div>
        </section>
      </div>
      <p className="demo-version-note">
        <Info size={14} />
        The AK3S version comes from the last installation attempt. Available releases are not checked automatically.
      </p>
      <section className="panel upgrade-plan">
        <div className="upgrade-plan-heading">
          <span className="guidance-icon">
            <ClipboardList size={21} />
          </span>
          <div>
            <h2>Plan your AK3S upgrade.</h2>
            <p>
              Back up your cluster, install the target AK3S CLI, then review and
              apply its pinned versions.
            </p>
          </div>
        </div>
        <div className="upgrade-actions">
          <button
            className="button button-primary"
            aria-expanded={stepsOpen}
            aria-controls="upgrade-steps"
            onClick={() => setStepsOpen(!stepsOpen)}
          >
            <Terminal size={16} />
            {stepsOpen ? "Hide upgrade steps" : "View upgrade steps"}
            <ChevronDown className={stepsOpen ? "rotate-up" : ""} size={15} />
          </button>
          <ExternalLinkButton
            className="button button-secondary"
            href={releasesUrl}
          >
            Browse releases
          </ExternalLinkButton>
        </div>
        {stepsOpen && (
          <div className="upgrade-steps" id="upgrade-steps">
            <ol>
              <li>
                <div>
                  <h3>Back up the cluster</h3>
                  <p>
                    Back up the datastore, server token, application volumes,
                    and AK3S configuration. Verify that you can restore them.
                  </p>
                  <ExternalLinkButton
                    className="text-link"
                    href={`${docsUrl}/operations.md#backups`}
                  >
                    Backup guide
                  </ExternalLinkButton>
                </div>
              </li>
              <li>
                <div>
                  <h3>Install the target AK3S release</h3>
                  <p>
                    Choose a published release and install its CLI on the
                    control-plane host. The upgrade command uses the installed
                    CLI's pinned versions.
                  </p>
                  <ExternalLinkButton
                    className="text-link"
                    href="https://github.com/jgador/ak3s#install"
                  >
                    Release installation
                  </ExternalLinkButton>
                </div>
              </li>
              <li>
                <div>
                  <h3>Preview, apply, and check</h3>
                  <p>
                    Review the dry-run output before running the upgrade. Run
                    each command separately.
                  </p>
                  <div className="command-heading">
                    <span>On the cluster host</span>
                    <CopyButton text={upgradeCommands} label="Copy commands" />
                  </div>
                  <CodeBlock code={upgradeCommands} />
                </div>
              </li>
            </ol>
            <div className="small-note">
              <Info size={16} />
              <p>
                For multiple control-plane servers, upgrade one at a time and
                check readiness and etcd quorum after each.
              </p>
            </div>
          </div>
        )}
      </section>
      <div className="upgrade-bottom">
        <p>
          <Info size={15} />
          Upgrades are performed through the AK3S CLI.
        </p>
        <ExternalLinkButton
          className="text-link"
          href={`${docsUrl}/operations.md#apply-and-upgrade`}
        >
          Read the upgrade guide
        </ExternalLinkButton>
      </div>
      <a className="back-link" href="#/overview">
        Back to overview
        <ArrowUpRight size={14} />
      </a>
    </>
  );
}
