import createMockInstance from 'jest-create-mock-instance';

import { LoggingService, LogMessage } from '../../../src/common/logging/logging.service';
import { PackageService } from '../../../src/common/package/package.service';

describe('PackageService', () => {
    let loggingService: LoggingService;

    beforeEach(() => {
        loggingService = createMockInstance(LoggingService);
    });

    it('should returns a PackageService instance', () => {
        // Arrange

        // Act
        const packageService = new PackageService(loggingService);

        // Assert
        expect(packageService).toBeDefined();
    });

    it('should get a Package', (done) => {
        // Arrange
        loggingService.trace = jest.fn((msg: LogMessage, ...args: any[]) => {
            expect(msg()).toContain(PackageService.name);
            done();
        });
        const packageService = new PackageService(loggingService);


        // Act
        const pkg = packageService.package;

        // Assert
        expect(pkg).toBeDefined();
        expect(pkg.name).toBe('@by-research/smh-template-connector-lib');
        expect(pkg.version).toBe('0.0.0-development');
    });
});
