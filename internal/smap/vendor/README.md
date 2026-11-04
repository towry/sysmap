# vendored

- `panzoom.min.js`: @panzoom/panzoom 4.6.2 (`dist/panzoom.min.js`, MIT, see `panzoom.LICENSE`; its header comment carries the copyright and stays in built pages). It gives the map drag-to-pan, pinch and wheel zoom and touch support. Update with `npm pack @panzoom/panzoom`.
- `dagre.min.js`: @dagrejs/dagre 3.1.3 (`dist/dagre.min.js`, MIT, see `dagre.LICENSE`). It lays out the node graph (layers, crossing reduction, edge routes) so the viewer does not hand-roll a layout. Update with `npm pack @dagrejs/dagre` and copy `dist/dagre.min.js` and `LICENSE`.
- `pele.global.js`: pele 1.13.0 (`dist/pele.global.js`, MIT, see `pele.LICENSE`). It renders the Mermaid diagrams referenced by a map. Only a page whose model has a diagram embeds it. Update with `npm pack pele@1` and copy `dist/pele.global.js` and `LICENSE`.
