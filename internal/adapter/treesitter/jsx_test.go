//go:build cgo

package treesitter

import "testing"

// viewpoint: behaviour
// target: the TypeScript reader
// Rendering a component of the repository calls it, written as an element or reached through a
// helper the file holds in a const, even one declared inside a describe block. HTML elements and
// components of other packages are not the repository's code.
func TestRenderingAComponentCallsIt(t *testing.T) {
	expect(t, calls(t, "typescript",
		"src/ui/Page.tsx", "export function Page() { return <main /> }\nexport function Badge() { return <span /> }\n",
		"src/ui/Page.test.tsx", `import { render } from '@testing-library/react'
import { Page } from './Page'
import * as ui from './Page'
import { Dialog } from 'some-library'

describe('page', () => {
  const renderPage = () => render(<Page />)

  it('direct', () => { render(<Page />) })
  it('helper', () => { renderPage() })
  it('namespace', () => { render(<ui.Badge>x</ui.Badge>) })
  it('html', () => { render(<div><Dialog /></div>) })
})
`), `page direct calls=[src/ui.Page] unresolved=0
page helper calls=[src/ui.Page] unresolved=0
page html calls=[] unresolved=0
page namespace calls=[src/ui.Badge] unresolved=0`)
}
