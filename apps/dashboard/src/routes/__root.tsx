import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Outlet, Link, createRootRouteWithContext } from "@tanstack/react-router";
import { AppSidebar } from "@/components/AppSidebar";
import { Toaster } from "@/components/ui/sonner";
import { useSoundNotifications } from "@/hooks/useSoundNotifications";

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  component: RootComponent,
  notFoundComponent: NotFound,
});

function NotFound() {
  return (
    <div className="min-h-screen flex items-center justify-center bg-background px-4">
      <div className="text-center">
        <p className="text-[10px] text-muted-foreground font-mono uppercase tracking-widest">
          Error 404
        </p>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight">页面未找到</h1>
        <Link
          to="/"
          className="mt-6 inline-flex px-4 py-2 rounded-md bg-brand text-brand-foreground text-sm font-medium hover:opacity-90 transition"
        >
          返回首页
        </Link>
      </div>
    </div>
  );
}

function RootComponent() {
  const { queryClient } = Route.useRouteContext();
  useSoundNotifications();
  return (
    <QueryClientProvider client={queryClient}>
      <div className="flex min-h-screen w-full bg-background text-foreground">
        <AppSidebar />
        <main className="flex-1 min-w-0 flex flex-col">
          <Outlet />
        </main>
      </div>
      <Toaster />
    </QueryClientProvider>
  );
}
