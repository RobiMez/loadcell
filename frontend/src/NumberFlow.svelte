<script lang="ts">
  // Wraps the vanilla `number-flow` web component so it plays nicely with
  // Svelte 3's attribute-based prop binding. Setting `value` as a property
  // (not an attribute) is what triggers the animated digit transitions.
  import { afterUpdate } from 'svelte';
  import 'number-flow';

  export let value: number = 0;
  export let format: Intl.NumberFormatOptions | undefined = undefined;
  export let prefix: string = '';
  export let suffix: string = '';
  export let locales: string | string[] | undefined = undefined;

  let el: any;

  afterUpdate(() => {
    if (!el) return;
    if (format !== undefined) el.format = format;
    if (locales !== undefined) el.locales = locales;
    if (prefix) el.numberPrefix = prefix;
    if (suffix) el.numberSuffix = suffix;
    // The custom element exposes update(value) to trigger the animated diff;
    // assigning `el.value = …` only sets a own property and won't render.
    const v = Number.isFinite(value) ? value : 0;
    el.update(v);
  });
</script>

<number-flow bind:this={el}></number-flow>
