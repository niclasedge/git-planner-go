// Einstiegspunkt des Svelte-Bundles.
//
// Randbedingung des Tickets (gkp-a1k): die alte Ansicht bleibt unveraendert
// erreichbar. browser.js ist eine IIFE ohne Abhaengigkeiten und raeumt die
// Seite nicht auf — beide Welten teilen sich dasselbe Dokument. Deshalb
// montiert dieses Bundle NUR in einen eigens dafuer vorgesehenen Knoten und
// tut sonst gar nichts: gibt es ihn nicht, passiert nichts, und keine
// bestehende Seite aendert ihr Verhalten, bloss weil das Script geladen wurde.
//
// Den Knoten rendert die Huelle unter /things (things-oberflaeche, #12).

import { mount } from 'svelte';
import App from './App.svelte';

const target = document.getElementById('kn-svelte-root');
if (target) {
  mount(App, { target });
}
