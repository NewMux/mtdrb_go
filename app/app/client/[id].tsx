// A client's profile lived here before the modules moved under /dashboard.
import { Redirect, useLocalSearchParams } from 'expo-router';

export default function Moved() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return <Redirect href={{ pathname: '/dashboard/clients/[id]', params: { id } }} />;
}
