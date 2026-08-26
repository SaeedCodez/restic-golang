import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Page } from "@/components/page";
import { EmptyState } from "@/components/empty";
import { Compass } from "lucide-react";

export default function NotFound() {
  return (
    <Page>
      <EmptyState
        icon={Compass}
        title="This page does not exist"
        description="The link may be out of date, or the thing it pointed at was deleted."
        action={
          <Button asChild>
            <Link to="/dashboard">Go to Dashboard</Link>
          </Button>
        }
      />
    </Page>
  );
}
