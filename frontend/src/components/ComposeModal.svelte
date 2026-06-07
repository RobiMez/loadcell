<script lang="ts">
  // Compose/edit a saved flow — picks an ordered set of saved requests and
  // persists them via SaveFlow. Owns its own form state (name + step list);
  // the parent passes the existing flow to edit (or null for a new one)
  // through `seed`, and gets the persisted result back via `onSaved`.
  import { X } from 'phosphor-svelte';
  import { SaveFlow } from '../../wailsjs/go/main/App.js';
  import type { main } from '../../wailsjs/go/models';
  import type { SavedFlowT } from '../types';

  export let open = false;
  export let requests: main.SavedRequest[] = [];
  export let seed: SavedFlowT | null = null;
  export let onSaved: (flow: SavedFlowT) => void = () => {};

  let editingId = '';
  let name = '';
  let stepIds: string[] = [];

  // Re-seed working state every time the modal flips open. Keying off
  // `open` (rather than `seed`) means that re-opening with the same seed
  // re-syncs (so a stale half-edited form doesn't survive close+reopen).
  $: if (open) {
    editingId = seed?.id || '';
    name = seed?.name || '';
    stepIds = seed ? [...seed.stepIds] : [];
  }

  // Saved requests pool. The same request can appear in the step list
  // multiple times (e.g. fetch → update → fetch again), so we don't filter
  // by used IDs.
  $: available = requests;

  function close() {
    open = false;
    editingId = '';
    name = '';
    stepIds = [];
  }

  function addStep(reqId: string) {
    stepIds = [...stepIds, reqId];
  }

  function removeStep(idx: number) {
    stepIds = stepIds.filter((_, i) => i !== idx);
  }

  function moveStep(idx: number, dir: -1 | 1) {
    const next = idx + dir;
    if (next < 0 || next >= stepIds.length) return;
    const copy = [...stepIds];
    [copy[idx], copy[next]] = [copy[next], copy[idx]];
    stepIds = copy;
  }

  async function save() {
    const trimmed = name.trim();
    if (!trimmed) return;
    if (stepIds.length === 0) return;
    try {
      const saved = (await SaveFlow({
        id: editingId,
        name: trimmed,
        stepIds,
        createdAt: '',
        updatedAt: '',
      } as any)) as unknown as SavedFlowT;
      onSaved(saved);
      close();
    } catch (e: any) {
      console.error('Save flow failed:', e);
    }
  }

  function onWindowKey(e: KeyboardEvent) {
    if (open && e.key === 'Escape') close();
  }
</script>

<svelte:window on:keydown={onWindowKey} />

{#if open}
  <div class="modal-backdrop" on:click|self={close} role="presentation">
    <div class="modal compose-modal" role="dialog" aria-modal="true" aria-label="Compose flow">
      <div class="modal-head">
        <h3 class="modal-title">{editingId ? 'Edit flow' : 'New flow'}</h3>
        <button class="modal-close" type="button" on:click={close} title="Close">
          <X size={14} weight="duotone" />
        </button>
      </div>
      <div class="modal-body">
        <label class="compose-name">
          <span class="k">Name</span>
          <input
            type="text"
            bind:value={name}
            placeholder="e.g. checkout-journey"
            spellcheck="false"
          />
        </label>
        <div class="compose-panes">
          <div class="compose-pane">
            <div class="compose-pane-head">
              <span class="compose-pane-title">Saved requests</span>
              <span class="compose-pane-hint">click to add →</span>
            </div>
            {#if available.length === 0}
              <p class="compose-empty">No saved requests yet.</p>
            {:else}
              <ul class="compose-avail">
                {#each available as r (r.id)}
                  <li>
                    <button class="compose-avail-row" type="button" on:click={() => addStep(r.id)}>
                      <span class="method m-{r.method.toLowerCase()}">{r.method}</span>
                      <span class="compose-avail-name">{r.name || 'Untitled'}</span>
                    </button>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
          <div class="compose-pane">
            <div class="compose-pane-head">
              <span class="compose-pane-title">Steps in order</span>
              <span class="compose-pane-hint">{stepIds.length} step{stepIds.length === 1 ? '' : 's'}</span>
            </div>
            {#if stepIds.length === 0}
              <p class="compose-empty">Pick requests on the left to build a flow.</p>
            {:else}
              <ol class="compose-steps">
                {#each stepIds as sid, i (i + ':' + sid)}
                  {@const r = requests.find((x) => x.id === sid)}
                  <li>
                    <span class="compose-step-num">{i + 1}</span>
                    {#if r}
                      <span class="method m-{r.method.toLowerCase()}">{r.method}</span>
                      <span class="compose-step-name">{r.name || 'Untitled'}</span>
                    {:else}
                      <span class="method m-deleted">DEL</span>
                      <span class="compose-step-name compose-step-missing">deleted request</span>
                    {/if}
                    <div class="compose-step-actions">
                      <button type="button" on:click={() => moveStep(i, -1)} disabled={i === 0} title="Move up" aria-label="Move up">↑</button>
                      <button type="button" on:click={() => moveStep(i, 1)} disabled={i === stepIds.length - 1} title="Move down" aria-label="Move down">↓</button>
                      <button type="button" on:click={() => removeStep(i)} title="Remove" aria-label="Remove">
                        <X size={11} weight="duotone" />
                      </button>
                    </div>
                  </li>
                {/each}
              </ol>
            {/if}
          </div>
        </div>
        <p class="compose-note">
          <strong>How flows run:</strong> Each worker fires step 1 → 2 → … → N in order, then loops. Workers are independent, so different workers may be on different steps at the same moment — no barrier between steps. Good for modeling real user journeys; not for "everyone fire step 1, then everyone fire step 2".
        </p>
      </div>
      <div class="modal-foot">
        <button class="btn btn-ghost" type="button" on:click={close}>Cancel</button>
        <button
          class="btn btn-primary"
          type="button"
          on:click={save}
          disabled={!name.trim() || stepIds.length === 0}
        >Save flow</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .compose-modal {
    width: min(880px, 92vw);
  }
  .compose-name {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .compose-name .k {
    font-size: 11px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .compose-name input {
    height: 34px;
    padding: 0 10px;
    background: var(--inset);
    color: var(--text);
    border: 1px solid var(--line-strong);
    border-radius: 4px;
    font: inherit;
    font-size: 13px;
  }
  .compose-name input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px rgba(159, 184, 173, 0.15);
  }
  .compose-note {
    margin: 0;
    padding: 10px 12px;
    background: var(--inset);
    border: 1px solid var(--line);
    border-left: 3px solid rgba(72, 89, 65, 0.35);
    border-radius: 4px;
    font-size: 11.5px;
    line-height: 1.55;
    color: var(--muted);
  }
  .compose-note strong {
    color: var(--text);
    font-weight: 600;
  }
  .compose-panes {
    display: grid;
    grid-template-columns: 1fr 1.1fr;
    gap: 12px;
    min-height: 320px;
  }
  .compose-pane {
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 6px;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .compose-pane-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 12px;
    border-bottom: 1px solid var(--line);
  }
  .compose-pane-title {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
  }
  .compose-pane-hint {
    font-size: 11px;
    color: var(--muted);
    opacity: 0.7;
  }
  .compose-empty {
    margin: 0;
    padding: 16px 12px;
    font-size: 12px;
    color: var(--muted);
    text-align: center;
  }
  .compose-avail {
    list-style: none;
    margin: 0;
    padding: 4px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    overflow-y: auto;
    flex: 1;
  }
  .compose-avail-row {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 7px 10px;
    background: transparent;
    border: none;
    border-radius: 4px;
    color: var(--text);
    font: inherit;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
    transition: background 120ms;
  }
  .compose-avail-row:hover {
    background: rgba(72, 89, 65, 0.10);
  }
  .compose-avail-name {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .compose-steps {
    list-style: none;
    margin: 0;
    padding: 4px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    overflow-y: auto;
    flex: 1;
  }
  .compose-steps li {
    display: grid;
    grid-template-columns: 22px auto 1fr auto;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    background: rgba(255, 255, 255, 0.7);
    border: 1px solid var(--line);
    border-radius: 4px;
  }
  .compose-step-num {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 20px;
    height: 20px;
    padding: 0 5px;
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 3px;
    font-size: 10.5px;
    font-weight: 600;
    color: var(--accent-strong);
    font-variant-numeric: tabular-nums;
  }
  .compose-step-name {
    font-size: 12.5px;
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .compose-step-missing {
    color: var(--err);
    font-style: italic;
  }
  .compose-step-actions {
    display: flex;
    gap: 2px;
  }
  .compose-step-actions button {
    appearance: none;
    background: transparent;
    border: 1px solid transparent;
    color: var(--muted);
    width: 22px;
    height: 22px;
    border-radius: 3px;
    cursor: pointer;
    font-size: 12px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .compose-step-actions button:hover:not(:disabled) {
    background: rgba(72, 89, 65, 0.10);
    border-color: rgba(72, 89, 65, 0.18);
    color: var(--text);
  }
  .compose-step-actions button:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }
  .compose-step-actions button:last-child:hover:not(:disabled) {
    background: rgba(120, 50, 50, 0.10);
    border-color: rgba(120, 50, 50, 0.20);
    color: var(--err);
  }

  /* Method badges (.method, .m-*) come from style.css. */
</style>
