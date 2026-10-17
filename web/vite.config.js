import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// Bau-Konfiguration der Svelte-Schiene (gkp-a1k).
//
// Drei Entscheidungen, alle aus den Randbedingungen des Tickets:
//
// 1. outDir zeigt DIREKT in den go:embed-Baum (internal/assets/static/app).
//    Kein Zwischenverzeichnis und kein Kopierschritt: ein Kopierschritt waere
//    eine zweite Stelle, an der der Stand auseinanderlaufen kann, und genau
//    davon hat dieses Repo schon genug (siehe die drei Kommentarbloecke im
//    justfile).
//
// 2. Feste Dateinamen statt Content-Hashes. Ein Hash im Namen erzwingt einen
//    Index, der den Namen nachschlaegt — also eine dritte bewegliche Stelle
//    zwischen Bau und Auslieferung. Bei einem eingebetteten Asset, das mit dem
//    Binary zusammen ausgetauscht wird, kauft der Hash nichts ein, was das
//    Binary nicht ohnehin schon garantiert. Ausserdem braucht //go:embed einen
//    Pfad, der zur Compile-Zeit feststeht.
//
// 3. iife statt ES-Modul. Das Bundle soll sich mit browser.js (ebenfalls IIFE,
//    ohne Abhaengigkeiten) dieselbe Seite teilen koennen, ohne dass die
//    einbettende Seite type="module", CORS oder Ladereihenfolge beachten muss.
export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: '../internal/assets/static/app',
    emptyOutDir: true,
    // Der Dienst laeuft als lokales Dashboard; eine Sourcemap kostet nur
    // Repo-Groesse in einem eingecheckten Artefakt.
    sourcemap: false,
    rollupOptions: {
      input: 'src/main.js',
      output: {
        format: 'iife',
        entryFileNames: 'app.js',
        assetFileNames: 'app[extname]',
      },
    },
  },
});
