import { toast } from "sonner";
import { errorOf } from "@/lib/api";
import { kindLabel } from "@/lib/runs";

/**
 * handleStartResponse turns the response of any "start a long-running
 * operation" endpoint into navigation or an explanation.
 *
 * The interesting case is contention: the server serializes operations per
 * repository and returns 409 `busy` naming the run that holds it. Rather than a
 * dead end, the user gets told which operation is in the way and can jump
 * straight to it.
 *
 * Pass `{ stay: true }` to keep the user on the current page (e.g. Job details)
 * and toast with an optional link to the new run instead of navigating away.
 */
export function handleStartResponse(res, navigate, fallback = "Could not start.", { stay = false } = {}) {
  if (res.status === 202 && res.body?.runId) {
    if (stay) {
      const kind = res.body.run?.kind ? kindLabel(res.body.run.kind) : "Operation";
      toast.success(`${kind} started`, {
        action: {
          label: "Open run",
          onClick: () => navigate(`/runs/${res.body.runId}`),
        },
      });
      return true;
    }
    navigate(`/runs/${res.body.runId}`);
    return true;
  }

  if (res.status === 409 && res.body?.code === "busy") {
    const blocking = res.body.blockingRun;
    const what = blocking ? kindLabel(blocking.kind).toLowerCase() : "another operation";
    toast.warning(`${res.body.repoName || "That repository"} is busy`, {
      description: `A ${what}${
        blocking?.jobName ? ` for “${blocking.jobName}”` : ""
      } is already running. Stop it or wait for it to finish.`,
      action: blocking?.runId
        ? { label: "View it", onClick: () => navigate(`/runs/${blocking.runId}`) }
        : undefined,
      duration: 8000,
    });
    return false;
  }

  if (res.body?.code === "no_restic") {
    toast.error("restic is not installed", { description: errorOf(res, fallback) });
    return false;
  }

  toast.error(errorOf(res, fallback));
  return false;
}
