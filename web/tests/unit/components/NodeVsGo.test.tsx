import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { NodeVsGo } from '../../../src/components/NodeVsGo';
import { PRISMA } from '../../../src/data';

describe('NodeVsGo', () => {
  it('renders the comparison heading and both TypeScript and Go columns', () => {
    const { container } = render(<NodeVsGo lib={PRISMA} />);
    expect(container.querySelector(`#${PRISMA.id}-cmp`)).not.toBeNull();
    expect(screen.getByText('TypeScript')).toBeInTheDocument();
    expect(screen.getByText('Go')).toBeInTheDocument();
    expect(container.querySelectorAll('.compare .code').length).toBe(2);
  });
});
