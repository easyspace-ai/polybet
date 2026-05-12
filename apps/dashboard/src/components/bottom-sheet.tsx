import { useEffect, useState } from 'react';

interface BottomSheetProps {
  open: boolean;
  onClose: () => void;
  children: React.ReactNode;
}

export function BottomSheet({ open, onClose, children }: BottomSheetProps) {
  const [mounted, setMounted] = useState(false);
  const [show, setShow] = useState(false);

  useEffect(() => {
    if (open) {
      setMounted(true);
      requestAnimationFrame(() => setShow(true));
    } else {
      setShow(false);
      const t = setTimeout(() => setMounted(false), 300);
      return () => clearTimeout(t);
    }
  }, [open]);

  if (!mounted) return null;

  return (
    <div className="fixed inset-0 z-50 md:hidden">
      <div
        className={[
          "absolute inset-0 bg-black/60 transition-opacity duration-300",
          show ? "opacity-100" : "opacity-0",
        ].join(" ")}
        onClick={onClose}
      />
      <div
        className={[
          "absolute bottom-0 left-0 right-0 bg-background border-t border-border rounded-t-xl transition-transform duration-300 ease-out max-h-[85vh] overflow-auto",
          show ? "translate-y-0" : "translate-y-full",
        ].join(" ")}
      >
        <div className="flex justify-center pt-2 pb-1">
          <div className="w-10 h-1 rounded-full bg-border" />
        </div>
        {children}
      </div>
    </div>
  );
}
