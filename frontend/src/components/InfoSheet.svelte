<script lang="ts">
  // Logo-click sheet: brand mark, one-paragraph blurb, the user-settable
  // max-concurrency cap, and Robi/sponsor links. Mostly static — the only
  // mutable piece is `maxWorkers`, which is two-way bound so the parent
  // can persist it to localStorage.
  import { X } from 'phosphor-svelte';
  import { BrowserOpenURL } from '../../wailsjs/runtime/runtime.js';
  import logoUrl from '../assets/images/loadcell.png';

  export let open = false;
  export let maxWorkers = 2000;
  export let maxWorkersFloor = 10;
  export let maxWorkersCeil = 20000;
  export let running = false;

  function close() {
    open = false;
  }

  function openRobiWork() {
    BrowserOpenURL('https://robi.work');
  }
  function openSponsor() {
    BrowserOpenURL('https://github.com/sponsors/RobiMez');
  }

  function onWindowKey(e: KeyboardEvent) {
    if (open && e.key === 'Escape') close();
  }
</script>

<svelte:window on:keydown={onWindowKey} />

{#if open}
  <div class="info-backdrop" role="presentation" on:click={close}>
    <div
      class="info-sheet"
      role="dialog"
      aria-modal="true"
      aria-labelledby="info-title"
      on:click|stopPropagation
    >
      <button type="button" class="info-close" on:click={close} aria-label="Close">
        <X size={14} weight="duotone" />
      </button>

      <div class="info-mark" aria-hidden="true">
        <img src={logoUrl} alt="" />
      </div>
      <h2 id="info-title" class="info-title">LoadCell</h2>
      <p class="info-subtitle">A desktop load tester for HTTP APIs.</p>

      <p class="info-body">
        Build requests, sketch a load profile, fire a test, then see throughput,
        latency percentiles, and per-status breakdowns over time. Runs are saved
        locally so you can switch between past tests.
      </p>

      <div class="info-setting">
        <label class="info-setting-label" for="info-max-workers">
          Max concurrency
          <span class="info-setting-hint">workers cap (default 500)</span>
        </label>
        <input
          id="info-max-workers"
          class="info-setting-input"
          type="number"
          min={maxWorkersFloor}
          max={maxWorkersCeil}
          step="50"
          bind:value={maxWorkers}
          disabled={running}
        />
        {#if maxWorkers > 2000}
          <p class="info-warn" role="alert">
            <strong>Above 2000.</strong> If your laptop melts or starts crying
            or your kernel panics, know that you brought it on yourself lol :) .
          </p>
        {/if}
      </div>

      <button type="button" class="info-link" on:click={openRobiWork} title="Open robi.work">
        Built by Robi · robi.work →
      </button>

      <button type="button" class="info-sponsor" on:click={openSponsor} title="Sponsor on GitHub">
        <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false">
          <path d="M12 21s-7-4.35-7-10a4 4 0 0 1 7-2.65A4 4 0 0 1 19 11c0 5.65-7 10-7 10Z" />
        </svg>
        Sponsor on GitHub
      </button>
    </div>
  </div>
{/if}

<style>
  .info-backdrop {
    position: fixed;
    inset: 0;
    z-index: 1000;
    background: rgba(31, 42, 29, 0.42);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 32px;
    animation: lc-info-fade-in 140ms ease-out;
  }
  .info-sheet {
    position: relative;
    width: min(440px, 100%);
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 12px;
    padding: 28px 28px 24px;
    box-shadow: 0 24px 64px rgba(31, 42, 29, 0.18);
    text-align: center;
    animation: lc-info-pop-in 180ms cubic-bezier(0.2, 0.7, 0.2, 1);
  }
  .info-close {
    position: absolute;
    top: 12px;
    right: 12px;
    appearance: none;
    background: transparent;
    border: 1px solid transparent;
    color: var(--muted);
    cursor: pointer;
    padding: 6px;
    border-radius: 4px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: all 120ms;
  }
  .info-close:hover {
    color: var(--text);
    border-color: var(--line);
    background: var(--surface);
  }
  .info-mark {
    width: 72px;
    height: 72px;
    margin: 0 auto 14px;
    border-radius: 16px;
    background: var(--inset);
    border: 1px solid var(--line);
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .info-mark img {
    width: 46px;
    height: 46px;
    object-fit: contain;
  }
  .info-title {
    margin: 0 0 4px;
    font-size: 20px;
    font-weight: 600;
    letter-spacing: -0.01em;
    color: var(--text);
  }
  .info-subtitle {
    margin: 0 0 14px;
    color: var(--muted);
    font-size: 13px;
  }
  .info-body {
    margin: 0 0 20px;
    color: var(--text);
    font-size: 13px;
    line-height: 1.6;
    text-align: left;
    padding: 12px 14px;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: 8px;
  }
  .info-setting {
    margin: 0 0 18px;
    padding: 12px 14px;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: 8px;
    text-align: left;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .info-setting-label {
    display: flex;
    align-items: baseline;
    gap: 8px;
    font-size: 12px;
    font-weight: 500;
    color: var(--text);
  }
  .info-setting-hint {
    color: var(--muted);
    font-size: 10px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }
  .info-setting-input {
    appearance: none;
    width: 100%;
    padding: 8px 10px;
    background: var(--inset);
    color: var(--text);
    border: 1px solid var(--line);
    border-radius: 4px;
    font: inherit;
    font-size: 13px;
    font-variant-numeric: tabular-nums;
    box-sizing: border-box;
  }
  .info-setting-input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 2px rgba(159, 184, 173, 0.15);
  }
  .info-warn {
    margin: 0;
    padding: 8px 10px;
    background: rgba(138, 94, 54, 0.10);
    border: 1px solid rgba(138, 94, 54, 0.40);
    border-radius: 4px;
    color: #5a3a1f;
    font-size: 11px;
    line-height: 1.5;
  }
  .info-warn strong {
    color: var(--err);
    font-weight: 700;
  }
  .info-link {
    appearance: none;
    background: var(--accent);
    color: #ffffff;
    border: 1px solid var(--accent);
    font: inherit;
    font-size: 12px;
    font-weight: 600;
    padding: 9px 18px;
    border-radius: 6px;
    cursor: pointer;
    transition: background 120ms, border-color 120ms;
  }
  .info-link:hover {
    background: var(--accent-strong);
    border-color: var(--accent-strong);
  }
  .info-sponsor {
    appearance: none;
    background: transparent;
    color: var(--text);
    border: 1px solid var(--line-strong);
    font: inherit;
    font-size: 12px;
    font-weight: 500;
    padding: 8px 16px;
    border-radius: 6px;
    cursor: pointer;
    margin-top: 8px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    transition: border-color 120ms, color 120ms, background 120ms;
  }
  .info-sponsor svg {
    width: 13px;
    height: 13px;
    color: #c44d4d;
  }
  .info-sponsor:hover {
    border-color: var(--accent);
    color: var(--accent-strong);
    background: rgba(72, 89, 65, 0.05);
  }
  .info-sponsor:hover svg {
    color: #b03939;
  }
  @keyframes lc-info-fade-in {
    from { opacity: 0; }
    to   { opacity: 1; }
  }
  @keyframes lc-info-pop-in {
    from { opacity: 0; transform: translateY(6px) scale(0.985); }
    to   { opacity: 1; transform: translateY(0) scale(1); }
  }
</style>
