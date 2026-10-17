import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

// Ohne diese Datei warnt vite-plugin-svelte bei jedem Bau ("no Svelte config
// found ... using default configuration"). Die Warnung ist harmlos, aber eine
// Warnung, die immer da ist, wird nicht mehr gelesen — und dann faellt die
// naechste, die es nicht ist, auch nicht mehr auf.
export default {
  preprocess: vitePreprocess(),
};
