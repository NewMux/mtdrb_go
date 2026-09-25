// The Clients tab lived here before the modules moved under /dashboard.
import { Redirect } from 'expo-router';

export default function Moved() {
  return <Redirect href="/dashboard/clients" />;
}
