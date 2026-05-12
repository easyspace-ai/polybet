import { toast as sonnerToast } from 'sonner';

interface ToastOptions {
  title?: string;
  description?: string;
  variant?: 'default' | 'success' | 'destructive';
}

export function toast({ title, description, variant = 'default' }: ToastOptions) {
  if (variant === 'success') {
    sonnerToast.success(title, { description });
  } else if (variant === 'destructive') {
    sonnerToast.error(title, { description });
  } else {
    sonnerToast(title, { description });
  }
}
