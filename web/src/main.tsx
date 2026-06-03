import React from "react";
import ReactDOM from "react-dom/client";
import { App } from "./ui/App";
import "./styles.css";

type ErrorBoundaryState = {
  errorMessage: string | null;
  errorStack: string;
};

class RootErrorBoundary extends React.Component<React.PropsWithChildren, ErrorBoundaryState> {
  state: ErrorBoundaryState = {
    errorMessage: null,
    errorStack: ""
  };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return {
      errorMessage: error.message || "Unknown renderer error",
      errorStack: error.stack || ""
    };
  }

  componentDidCatch(error: Error) {
    console.error("renderer crash", error);
  }

  render() {
    if (this.state.errorMessage) {
      return (
        <div className="renderer-error-shell">
          <div className="renderer-error-card">
            <h1>Renderer Error</h1>
            <p>The desktop UI hit a runtime error instead of rendering the app.</p>
            <pre>{this.state.errorMessage}</pre>
            {this.state.errorStack && <pre>{this.state.errorStack}</pre>}
          </div>
        </div>
      );
    }

    return this.props.children;
  }
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <RootErrorBoundary>
      <App />
    </RootErrorBoundary>
  </React.StrictMode>
);
