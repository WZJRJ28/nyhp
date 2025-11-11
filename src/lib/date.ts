import { format, formatDistanceToNow } from 'date-fns';

export function formatDate(value: string | number | Date, template = 'yyyy-MM-dd HH:mm') {
  return format(new Date(value), template);
}

export function fromNow(value: string | number | Date) {
  return formatDistanceToNow(new Date(value), { addSuffix: true });
}
