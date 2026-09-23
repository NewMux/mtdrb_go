import { arcPath, areaPath, barPath, bands, linearScale, linePath, niceTicks, thinLabels } from '@/ui/charts/geometry';

describe('chart geometry', () => {
  it('picks round ticks that start at zero', () => {
    expect(niceTicks(12, 947)).toEqual([0, 200, 400, 600, 800, 1000]);
    expect(niceTicks(0, 0)).toEqual([0, 0.2, 0.4, 0.6, 0.8, 1]);
    expect(niceTicks(-30, 80)[0]).toBeLessThanOrEqual(-30);
  });

  it('scales linearly, inverted for a y axis', () => {
    const y = linearScale([0, 100], [200, 0]);
    expect(y(0)).toBe(200);
    expect(y(50)).toBe(100);
  });

  it('breaks lines and areas at gaps', () => {
    const pts = [{ x: 0, y: 10 }, { x: 10, y: 5 }, null, { x: 30, y: 0 }];
    expect(linePath(pts)).toBe('M0,10L10,5M30,0');
    expect(areaPath(pts, 20).match(/Z/g)).toHaveLength(2);
  });

  it('draws nothing for a zero bar rather than a sliver', () => {
    expect(barPath(0, 10, 100, 100)).toBe('');
    expect(barPath(0, 10, 100, 40)).toContain('Q');
  });

  it('closes a full donut ring', () => {
    expect(arcPath(50, 50, 40, 30, 0, Math.PI * 2).match(/Z/g)).toHaveLength(2);
  });

  it('spaces bands and thins crowded labels, keeping the last', () => {
    const b = bands(4, 400, 0.2);
    expect(b.centre(0)).toBe(50);
    expect(b.inner).toBe(80);
    const kept = thinLabels(30, 300);
    expect(kept[0]).toBe(0);
    expect(kept.at(-1)).toBe(29);
    expect(kept.length).toBeLessThan(30);
  });
});
