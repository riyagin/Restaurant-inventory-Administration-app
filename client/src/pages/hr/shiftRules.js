// Shift helpers shared by manpower planning and its shift components.

export const shiftTimeLabel = (s) =>
  `${s.start1}–${s.end1}${s.start2 ? `, ${s.start2}–${s.end2}` : ''}`;

export const shiftHoursLabel = (s) => {
  const h = s.work_minutes / 60;
  return `${Number.isInteger(h) ? h : h.toLocaleString('id-ID', { maximumFractionDigits: 1 })} jam`;
};

/** The division a day counts towards: the day's override, else home division. */
export const dayDivisionId = (emp, day) => day.division_id || emp.division_id || '';

export const NO_DIVISION = '__none__';

/**
 * Splits a branch's employees into division sections, in the branch's division
 * order, with the unassigned last. Sections without anyone are left out.
 */
export function groupByHomeDivision(branch) {
  const sections = branch.divisions.map(d => ({ id: d.id, name: d.name, employees: [] }));
  const none = { id: NO_DIVISION, name: 'Tanpa divisi', employees: [] };
  const byId = Object.fromEntries(sections.map(s => [s.id, s]));
  for (const emp of branch.employees) (byId[emp.division_id] || none).employees.push(emp);
  return [...sections, none].filter(s => s.employees.length > 0);
}
