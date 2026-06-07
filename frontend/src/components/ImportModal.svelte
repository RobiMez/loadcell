<script lang="ts">
  // Drop-file modal that parses a result file from k6 or vegeta into a
  // SavedRun via the Go-side ImportRun binding, then hands the persisted
  // run back to the parent through `onImported`. Self-contained: owns its
  // own drag state, file ref, error surface, and Escape handling.
  import { X, CircleNotch, UploadSimple } from 'phosphor-svelte';
  import { ImportRun } from '../../wailsjs/go/main/App.js';
  import type { Run } from '../types';

  export let open = false;
  export let onImported: (run: Run) => void = () => {};

  let inputEl: HTMLInputElement | null = null;
  let importing = false;
  let error = '';
  let dragActive = false;

  // Reset transient state every time the modal flips open so a stale
  // error from a previous attempt doesn't greet the user on reopen.
  $: if (open) {
    error = '';
    dragActive = false;
  }

  function close() {
    if (importing) return;
    open = false;
    error = '';
    dragActive = false;
  }

  function pickFile() {
    error = '';
    inputEl?.click();
  }

  async function importFromFile(file: File) {
    importing = true;
    error = '';
    try {
      const text = await file.text();
      const persisted = (await ImportRun(file.name, text)) as unknown as Run;
      onImported(persisted);
      open = false;
    } catch (err) {
      error = String((err as any)?.message ?? err);
      console.error('ImportRun failed:', err);
    } finally {
      importing = false;
    }
  }

  async function onFile(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = ''; // allow re-importing the same file
    if (!file) return;
    await importFromFile(file);
  }

  function onDragOver(e: DragEvent) {
    e.preventDefault();
    if (importing) return;
    dragActive = true;
  }

  function onDragLeave(e: DragEvent) {
    e.preventDefault();
    dragActive = false;
  }

  async function onDrop(e: DragEvent) {
    e.preventDefault();
    dragActive = false;
    if (importing) return;
    const file = e.dataTransfer?.files?.[0];
    if (!file) return;
    await importFromFile(file);
  }

  function onWindowKey(e: KeyboardEvent) {
    if (open && e.key === 'Escape') close();
  }
</script>

<svelte:window on:keydown={onWindowKey} />

{#if open}
  <div class="modal-backdrop" on:click|self={close} role="presentation">
    <div class="modal import-modal" role="dialog" aria-modal="true" aria-label="Import results">
      <div class="modal-head">
        <h3 class="modal-title">Import results</h3>
        <button class="modal-close" type="button" on:click={close} title="Close" disabled={importing}>
          <X size={14} weight="duotone" />
        </button>
      </div>
      <div class="modal-body">
        <p class="import-lede">
          Drop a metrics JSON file produced by <strong>k6</strong> or <strong>vegeta</strong>. LoadCell will parse it and visualise the run with the same charts as a native test.
        </p>

        <div
          class="import-dropzone"
          class:active={dragActive}
          class:busy={importing}
          on:dragover={onDragOver}
          on:dragleave={onDragLeave}
          on:drop={onDrop}
          on:click={pickFile}
          on:keydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); pickFile(); } }}
          role="button"
          tabindex="0"
          aria-label="Drop a results file or click to browse"
        >
          <div class="import-dz-icon" aria-hidden="true">
            {#if importing}
              <CircleNotch size={28} class="spin" />
            {:else}
              <UploadSimple size={28} weight="duotone" />
            {/if}
          </div>
          <div class="import-dz-main">
            {#if importing}
              Importing…
            {:else if dragActive}
              Release to import
            {:else}
              <strong>Drop file here</strong> or <span class="import-dz-link">click to browse</span>
            {/if}
          </div>
          <div class="import-dz-hint">.json · .ndjson · max one file</div>
        </div>

        <input
          type="file"
          accept=".json,.ndjson,.txt,application/json,text/plain"
          class="hidden-file-input"
          bind:this={inputEl}
          on:change={onFile}
        />

        {#if error}
          <p class="import-error" role="alert">{error}</p>
        {/if}

        <div class="import-formats">
          <div class="import-format">
            <div class="import-format-head">
              <span class="import-format-name">k6</span>
              <span class="import-format-hint">JSON metrics stream or summary export</span>
            </div>
            <pre class="import-format-cmd">k6 run --out json=out.json script.js</pre>
            <p class="import-format-alt">
              Or a summary file from <code>handleSummary()</code> / <code>--summary-export=summary.json</code>.
            </p>
          </div>
          <div class="import-format">
            <div class="import-format-head">
              <span class="import-format-name">vegeta</span>
              <span class="import-format-hint">per-request NDJSON (recommended)</span>
            </div>
            <pre class="import-format-cmd">vegeta attack -targets=t.txt -duration=30s &gt; r.bin
vegeta encode -to=json r.bin &gt; r.json</pre>
            <p class="import-format-alt">
              Or a summary from <code>vegeta report -type=json</code> — flat timeline, no endpoint.
            </p>
          </div>
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn btn-ghost" type="button" on:click={close} disabled={importing}>Close</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .import-modal {
    width: min(640px, 92vw);
  }
  .import-lede {
    margin: 0;
    font-size: 13px;
    line-height: 1.55;
    color: var(--muted);
  }
  .import-lede strong {
    color: var(--text);
    font-weight: 600;
  }
  .import-dropzone {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 28px 18px;
    border: 1.5px dashed var(--line-strong);
    border-radius: 6px;
    background: var(--inset);
    color: var(--muted);
    cursor: pointer;
    text-align: center;
    transition: border-color 120ms ease, background 120ms ease, color 120ms ease;
    user-select: none;
  }
  .import-dropzone:hover:not(.busy),
  .import-dropzone:focus-visible {
    border-color: var(--accent);
    color: var(--text);
    outline: none;
  }
  .import-dropzone.active {
    border-color: var(--accent-strong);
    background: rgba(159, 184, 173, 0.14);
    color: var(--text);
  }
  .import-dropzone.busy {
    cursor: default;
  }
  .import-dz-icon {
    color: var(--accent-strong);
    display: inline-flex;
  }
  .import-dz-main {
    font-size: 13.5px;
    line-height: 1.4;
  }
  .import-dz-main strong {
    color: var(--text);
    font-weight: 600;
  }
  .import-dz-link {
    color: var(--accent-strong);
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .import-dz-hint {
    font-size: 11px;
    color: var(--muted-2, var(--muted));
    letter-spacing: 0.02em;
  }
  .hidden-file-input {
    position: absolute;
    width: 1px;
    height: 1px;
    opacity: 0;
    pointer-events: none;
  }
  .import-error {
    color: #c0563b;
    font-size: 12px;
    margin: 0;
    padding: 8px 10px;
    line-height: 1.5;
    word-break: break-word;
    background: rgba(192, 86, 59, 0.08);
    border: 1px solid rgba(192, 86, 59, 0.25);
    border-radius: 4px;
  }
  .import-formats {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }
  @media (max-width: 620px) {
    .import-formats {
      grid-template-columns: 1fr;
    }
  }
  .import-format {
    padding: 12px;
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 4px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .import-format-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
  }
  .import-format-name {
    font-size: 12.5px;
    font-weight: 700;
    color: var(--text);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
  .import-format-hint {
    font-size: 11px;
    color: var(--muted);
  }
  .import-format-cmd {
    margin: 0;
    padding: 8px 10px;
    background: rgba(20, 30, 25, 0.06);
    border: 1px solid var(--line);
    border-radius: 3px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 11.5px;
    line-height: 1.5;
    color: var(--text);
    white-space: pre-wrap;
    word-break: break-word;
  }
  .import-format-alt {
    margin: 0;
    font-size: 11.5px;
    line-height: 1.5;
    color: var(--muted);
  }
  .import-format-alt code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 11px;
    padding: 1px 4px;
    background: rgba(20, 30, 25, 0.06);
    border-radius: 3px;
    color: var(--text);
  }
</style>
