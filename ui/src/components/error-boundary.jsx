import * as React from "react";
import { Button } from "@/components/ui/button";

/**
 * ErrorBoundary keeps a rendering bug in one screen from leaving a blank page
 * with no way out — the durable state is all server-side, so a reload always
 * recovers.
 */
export class ErrorBoundary extends React.Component {
  constructor(props) {
    super(props);
    this.state = { error: null };
  }

  static getDerivedStateFromError(error) {
    return { error };
  }

  componentDidCatch(error, info) {
    console.error("UI error:", error, info);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div className="mx-auto flex min-h-dvh max-w-md flex-col items-center justify-center gap-4 px-6 text-center">
        <h1 className="text-lg font-semibold">Something broke in the interface</h1>
        <p className="text-[13px] leading-relaxed text-muted-foreground">
          Your backups and their history are stored by the server, not the browser, so
          nothing has been lost. Reloading should restore the page.
        </p>
        <pre className="max-h-40 w-full overflow-auto scroll-thin rounded-md border border-border bg-log-bg p-3 text-left font-mono text-xs">
          {String(this.state.error?.message || this.state.error)}
        </pre>
        <Button onClick={() => window.location.reload()}>Reload</Button>
      </div>
    );
  }
}
