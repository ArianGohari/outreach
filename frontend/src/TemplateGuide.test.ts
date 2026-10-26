import { render, screen, fireEvent } from '@testing-library/svelte';
import { expect, it } from 'vitest';
import TemplateGuide from './TemplateGuide.svelte';

it('lists the eight agreed CSV fields and the company alias before import', async () => {
  render(TemplateGuide);
  await fireEvent.click(screen.getByText('Template-Tags & Anleitung'));
  const table = screen.getByRole('table', { name: 'Verfügbare Template-Tags' });
  expect(table.querySelectorAll('tbody tr')).toHaveLength(9);
  for (const name of [
    'First Name',
    'Last Name',
    'Title',
    'Company Name',
    'Email',
    'Contact Owner',
    'Person Linkedin Url',
    'City',
    'Company',
  ]) {
    expect(table.textContent).toContain('{{' + name + '}}');
  }
});

it('lists every imported column without suggesting absent fields', async () => {
  render(TemplateGuide, { headers: ['First Name', 'Email', 'Custom Field'] });
  await fireEvent.click(screen.getByText('Template-Tags & Anleitung'));
  const table = screen.getByRole('table', { name: 'Verfügbare Template-Tags' });
  expect(table.querySelectorAll('tbody tr')).toHaveLength(3);
  expect(table.textContent).toContain('{{Custom Field}}');
  expect(table.textContent).not.toContain('{{Company}}');
  expect(table.textContent).not.toContain('{{Industry}}');
});
