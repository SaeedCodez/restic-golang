import * as React from "react";
import { Link } from "react-router-dom";
import { Compass, LayoutGrid } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Page } from "@/components/page";
import { EmptyState } from "@/components/empty";

export default function NotFound() {
  React.useEffect(() => {
    document.title = "Not found · restic backup manager";
  }, []);

  return (
    <Page>
      <EmptyState
        framed
        icon={Compass}
        tone="brand"
        title="This page does not exist"
        description="The link may be out of date, or the thing it pointed at was deleted."
        className="py-20"
        action={
          <>
            <Button asChild>
              <Link to="/dashboard">
                <LayoutGrid />
                Go to Dashboard
              </Link>
            </Button>
            <Button variant="outline" onClick={() => window.history.back()}>
              Go back
            </Button>
          </>
        }
      />
    </Page>
  );
}
