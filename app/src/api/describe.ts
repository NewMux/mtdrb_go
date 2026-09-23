/**
 * Turns a failed request into a sentence a trainer can act on.
 *
 * One place, so that every screen that talks to the server answers the same
 * refusal the same way, in both languages, and none of them shows the
 * server's English message to someone reading the app in Arabic. The server's
 * message is the last resort, for a code nothing here knows yet.
 */

import { ApiError, DEMO_READ_ONLY, NetworkError } from '@/api/client';
import type { I18n, TKey } from '@/i18n';

export function describeError(cause: unknown, { t }: I18n, offline: TKey = 'errors.offline'): string {
  if (cause instanceof NetworkError) return t(offline);
  if (!(cause instanceof ApiError)) {
    return cause instanceof Error ? cause.message : t('common.somethingWrong');
  }
  switch (cause.code) {
    case DEMO_READ_ONLY:
      return t('demo.readOnly');
    case 'rate_limited':
      return t('errors.rateLimited');
    case 'subscription_inactive':
      return t('plan.readOnlyBody');
    case 'plan_limit_reached':
      return t('plan.limitReached', { count: Number(cause.meta?.max ?? 0) });
    case 'feature_not_in_plan':
      return t('plan.featureNotInPlan');
    case 'currency_locked':
      return t('errors.currencyLocked');
    case 'invalid_mfa_code':
      return t('security.wrongCode');
    case 'reset_link_invalid':
      return t('resetPassword.linkInvalid');
    case 'invalid_credentials':
      return cause.fields?.current_password ? t('errors.wrongPassword') : t('signIn.badCredentials');
    case 'email_taken':
      return t('errors.emailTaken');
    case 'validation_failed': {
      const fields = cause.fields ? Object.values(cause.fields) : [];
      return fields[0] ?? cause.message;
    }
    default:
      return cause.message;
  }
}
