import { describeUserAgent } from '@/features/devices';
import { cleanHours, normaliseTime, weekOrder } from '@/features/working-hours';
import { qrPath } from '@/ui/qr-path';

describe('working hours', () => {
  it('normalises the times people type', () => {
    expect(normaliseTime('9')).toBe('09:00');
    expect(normaliseTime('9:30')).toBe('09:30');
    expect(normaliseTime('0630')).toBe('06:30');
    expect(normaliseTime('24:00')).toBeNull();
    expect(normaliseTime('noon')).toBeNull();
  });

  it('runs the week from its first day', () => {
    expect(weekOrder(0)).toEqual(['0', '1', '2', '3', '4', '5', '6']);
    expect(weekOrder(1)).toEqual(['1', '2', '3', '4', '5', '6', '0']);
  });

  it('cleans a week for the server and names the day that is wrong', () => {
    expect(cleanHours({ '1': [['16', '20:00'], ['6:00', '12']], '5': [] }))
      .toEqual({ ok: true, hours: { '1': [['06:00', '12:00'], ['16:00', '20:00']] } });
    expect(cleanHours({ '2': [['10:00', '09:00']] })).toEqual({ ok: false, day: '2' });
    expect(cleanHours({ '3': [['06:00', '10:00'], ['09:00', '11:00']] })).toEqual({ ok: false, day: '3' });
  });
});

describe('device names', () => {
  it('names browsers and machines the way their owner would', () => {
    expect(describeUserAgent('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1'))
      .toEqual({ browser: 'Safari', system: 'iPhone', kind: 'phone' });
    expect(describeUserAgent('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36'))
      .toEqual({ browser: 'Chrome', system: 'macOS', kind: 'computer' });
    expect(describeUserAgent('okhttp/4.12.0')).toEqual({ browser: null, system: null, kind: 'phone' });
    expect(describeUserAgent('')).toBeNull();
    expect(describeUserAgent('Go-http-client/1.1')).toBeNull();
  });
});

describe('QR codes', () => {
  it('draws a square code with a quiet zone', () => {
    const { size, d } = qrPath('otpauth://totp/CoachPulse:sam@example.com?secret=JBSWY3DPEHPK3PXP&issuer=CoachPulse');
    expect(size).toBeGreaterThanOrEqual(21 + 8);
    // The finder pattern's corner module sits just inside the quiet zone.
    expect(d.startsWith('M4 4h1v1h-1z')).toBe(true);
  });
});
