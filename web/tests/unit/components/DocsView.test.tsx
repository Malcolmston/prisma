import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { DocsView } from '../../../src/components/DocsView';
import type { DocIndex } from 'go-ui';

// A minimal DocIndex the stubbed fetch returns for DocsApp's doc.json request.
const DOC_INDEX: DocIndex = {
  module: 'github.com/malcolmston/prisma',
  packages: [
    {
      importPath: 'github.com/malcolmston/prisma',
      name: 'prisma',
      synopsis: 'Package prisma is a standard-library-only, type-safe query builder and ORM over database/sql.',
      doc: 'Package prisma is a standard-library-only, type-safe query builder and ORM over database/sql.',
      consts: [],
      vars: [],
      types: [
        {
          name: 'Model',
          signature: 'type Model struct{}',
          doc: 'Model is a struct compiled from prisma struct tags via reflection.',
          consts: [],
          vars: [],
          funcs: [],
          methods: [],
        },
      ],
      funcs: [{ name: 'NewClient', signature: 'func NewClient(db *sql.DB, opts ...Option) *Client', doc: 'NewClient wraps a *sql.DB.' }],
    },
  ],
};

describe('DocsView', () => {
  beforeEach(() => {
    // DocsApp fetches doc.json; return the small index.
    global.fetch = vi.fn((input: RequestInfo | URL) => {
      if (String(input).includes('doc.json')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(DOC_INDEX) } as Response);
      }
      return new Promise<Response>(() => {});
    }) as unknown as typeof fetch;
  });

  it('renders the inline React API reference from the fetched doc.json', async () => {
    const { container } = render(<DocsView />);
    expect(container.querySelector('#view-docs')).not.toBeNull();
    expect(
      screen.getByRole('heading', { level: 2, name: /API documentation/ }),
    ).toBeInTheDocument();

    // DocsApp fetches asynchronously, then renders the package view + symbols.
    expect(await screen.findByRole('heading', { name: /package prisma/ })).toBeInTheDocument();
    expect(container.querySelector('#sym-NewClient'), 'func NewClient symbol card').not.toBeNull();
    expect(container.querySelector('#sym-Model'), 'type Model symbol card').not.toBeNull();

    // The secondary link to the raw generated static HTML remains.
    expect(screen.getByRole('link', { name: /Open the raw generated HTML/ })).toHaveAttribute('href', './api/');
  });
});
