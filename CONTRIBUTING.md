# How to contribute

We are glad you are interested in contributing to this project! We welcome contributions from anyone on the internet, and are grateful for even the smallest of fixes! There are many ways to contribute, from writing tutorials, improving the documentation, submitting bug reports and feature requests or writing code which can be incorporated into the main project itself. However, we request you to follow some basic guidelines which are mentioned below.

## **Submit an issue**
Before you submit a new issue, check the existing ones to ensure yours is new. If the issue is already logged, you can express your token support by adding a reaction. This will help maintainers to identify popular issues quickly.


## **Branch out**
Fork the repository and make your changes on a new branch, branched out from `dev`.

## **Making Changes** 
Once you have made the required changes, make sure you have tested your changes well. Commit your changes with a message which clearly explains the changes you have made, and try to follow the given format:

    <major_area>...: <type>: <commit-message>
    
    <commit-description>

**Available major areas**:
- `core` - For changes in the core (warplib).
- `docs` - For changes in the documentation.
- `api` - For changes in the API specifications.
- `daemon` - For changes in the daemon.
- `extl` - For changes in the extension loader.

**Note**: If your changes fall under multiple major areas, you can use multiple major areas separated by a comma.

**Available types**:
- `feat` - For adding a new feature.
- `fix` - For a bug fix.
- `refactor` - For a code refactor.
- `perf` - For a performance improvement.
- `test` - For a test case.
- `chore` - For a chore commit.

**Examples**:
1. Suppose you have added a feature 'X' in the warplib that includes changes to API as well, then the commit would look like:

    ```
    core,daemon: feat: implemented 'X'

    This commit adds the 'X' feature that performs....
    ```

2. For making changes to the documentations, a commit would look like this:

    ```
    docs: fix a typo in 'X' section.
    ```


## **Making a PR** 
In order to make a PR, follow the mentioned below steps:
-  Please make sure your commits the guidelines mentioned above.
- Once you have committed your changes, push your changes to the branch.
- Now, go to the original repository and you will see a `Compare & pull request` button. Click on that button.
- Add a title and description to your PR explaining the changes you have made.
- Sit back and relax while the maintainers review your PR. Please understand that there will be reviews and you may need to make some changes to your PR.

Thanks for reading the guidelines!
Feel free to ping us in the [discussions](https://github.com/orgs/warpdl/discussions) for any query.