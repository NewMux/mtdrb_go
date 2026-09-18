import {
  money, load, rpe, bodyFat, rest, repRange, dueLabel,
  parseLoad, parseMoney, parseRpe, parseReps, parseBodyFat,
} from '@/ui/format';

describe('formatting', () => {
  it('renders money from minor units without floating point', () => {
    expect(money(50000, 'EUR')).toBe('500.00 EUR');
    expect(money(5, 'USD')).toBe('0.05 USD');
    expect(money(0, 'GBP')).toBe('0.00 GBP');
    // An overdrawn balance is shown as owed, not hidden.
    expect(money(-2500, 'EUR')).toBe('-25.00 EUR');
  });

  it('converts loads only at the edge', () => {
    // Stored in grams so it never rounds; 100kg is exactly 100000.
    expect(load(100000)).toBe('100 kg');
    expect(load(102500)).toBe('102.5 kg');
    expect(load(100000, 'lb')).toBe('220.5 lb');
    expect(load(null)).toBe('—');
  });

  it('renders RPE from tenths', () => {
    expect(rpe(85)).toBe('8.5');
    expect(rpe(100)).toBe('10');
    expect(rpe(null)).toBe('—');
  });

  it('renders body fat from basis points', () => {
    expect(bodyFat(1550)).toBe('15.5%');
    expect(bodyFat(2000)).toBe('20%');
  });

  it('renders rest intervals the way a trainer says them', () => {
    expect(rest(45)).toBe('45s');
    expect(rest(120)).toBe('2m');
    expect(rest(150)).toBe('2:30');
  });

  it('renders prescribed rep ranges', () => {
    expect(repRange(6, 8)).toBe('6–8');
    expect(repRange(8, 8)).toBe('8');
    expect(repRange(5, null)).toBe('5');
  });

  it('says how overdue an invoice is in plain words', () => {
    const today = new Date('2026-05-10T12:00:00Z');
    expect(dueLabel('2026-05-07', today)).toBe('3 days overdue');
    expect(dueLabel('2026-05-10', today)).toBe('due today');
    expect(dueLabel('2026-05-15', today)).toBe('due in 5 days');
    expect(dueLabel('2026-05-09', today)).toBe('1 day overdue');
  });
});

describe('parsing what a trainer types', () => {
  it('converts kilos to grams without floating-point drift', () => {
    expect(parseLoad('82.5')).toBe(82_500);
    expect(parseLoad('0.5')).toBe(500);
    expect(parseLoad('100')).toBe(100_000);
  });

  it('accepts a comma as the decimal separator', () => {
    // A European phone keyboard offers a comma, and "72,5" means seventy-two
    // and a half — not nothing.
    expect(parseLoad('72,5')).toBe(72_500);
    expect(parseMoney('120,50')).toBe(12_050);
  });

  it('returns null for a half-typed number rather than NaN', () => {
    for (const input of ['', ' ', '.', '-', 'abc', '12kg', '1.2.3']) {
      expect(parseLoad(input)).toBeNull();
      expect(parseMoney(input)).toBeNull();
    }
  });

  it('rejects an RPE outside the scale instead of clamping it', () => {
    expect(parseRpe('8.5')).toBe(85);
    expect(parseRpe('10')).toBe(100);
    // Clamping 12 to 10 would silently record something the trainer did not
    // mean; an empty field is honest.
    expect(parseRpe('12')).toBeNull();
    expect(parseRpe('-1')).toBeNull();
  });

  it('refuses a fractional rep', () => {
    expect(parseReps('12')).toBe(12);
    expect(parseReps('12.5')).toBeNull();
  });

  it('round-trips through the formatters', () => {
    expect(load(parseLoad('82.5'))).toBe('82.5 kg');
    expect(rpe(parseRpe('8.5'))).toBe('8.5');
    expect(money(parseMoney('120.50') ?? 0, 'EUR')).toBe('120.50 EUR');
    expect(bodyFat(parseBodyFat('15.5'))).toBe('15.5%');
  });

  it('converts pounds for a trainer working in them', () => {
    expect(parseLoad('225', 'lb')).toBe(102_058);
    expect(load(parseLoad('225', 'lb'), 'lb')).toBe('225 lb');
  });
});
