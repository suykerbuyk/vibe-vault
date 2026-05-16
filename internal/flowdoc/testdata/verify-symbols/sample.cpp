// C++ fixture for verify symbol-grammar tests.

#include <string>

class CanMonitor {
public:
    CanMonitor();
    void start();
};

namespace recmeet {

std::string standalone_main(int argc, char** argv) {
    return "ok";
}

void run_pipeline(CanMonitor& mon) {
    /* result = run_pipeline(args); -- call site, must not match */
    mon.start();
}

} // namespace recmeet
