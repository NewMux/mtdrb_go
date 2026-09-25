/**
 * The account-level wire types: settings, the plan, the trainer's profile and
 * devices. Taken straight from the generated spec, per the note in
 * conformance.ts. `Required` because the spec lists every field the server
 * always sends without marking it required; the ones that can be absent are
 * nullable there, and stay so here.
 */

import type { Schema } from './conformance';

export type Settings = Required<Schema<'Settings'>>;
export type SettingsPatch = Schema<'SettingsPatch'>;
export type WorkingHours = Schema<'WorkingHours'>;
export type Targets = Schema<'Targets'>;
export type Subscription = Required<Schema<'Subscription'>>;
export type Profile = Required<Schema<'Profile'>>;
export type Device = Required<Schema<'Device'>>;
export type MFASetup = Required<Schema<'MFASetup'>>;
export type Location = Required<Schema<'Location'>>;
export type LocationInput = Schema<'LocationInput'>;
export type PackageOffer = Required<Schema<'PackageOffer'>>;
export type PackageOfferInput = Schema<'PackageOfferInput'>;
