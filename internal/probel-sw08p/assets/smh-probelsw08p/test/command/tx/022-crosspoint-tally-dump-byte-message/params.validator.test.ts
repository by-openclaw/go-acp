import { BootstrapService } from '../../../../src/bootstrap.service';
import { CrossPointTallyDumpByteCommandParams } from '../../../../src/command/tx/022-crosspoint-tally-dump-byte-message/params';
import { LocaleData } from '../../../../src/common/locale-data/locale-data.model';
import { CommandParamsValidator } from '../../../../src/command/tx/022-crosspoint-tally-dump-byte-message/params.validator';
import { CrossPointTallyDumpByteCommand } from '../../../../src/command/tx/022-crosspoint-tally-dump-byte-message/command';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('CrossPoint Tally Dump (byte) Message - CommandParamsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            // generate an array of sourceIdItems
            const buildDataArray = new Array<number>();
            for (let itemIndex = 0; itemIndex < 191; itemIndex++) {
                // Add the sourceId Items buffer to the array
                buildDataArray.push(itemIndex);
            }

            const params: CrossPointTallyDumpByteCommandParams = {
                matrixId: 0,
                levelId: 0,
                numberOfTalliesReturned: buildDataArray.length,
                firstDestinationId: 0,
                sourceIdItems: buildDataArray
            };

            // Act

            const validator = new CommandParamsValidator(params);
            const command = new CrossPointTallyDumpByteCommand(params);
            command.buildCommand();

            // Assert
            expect(validator).toBeDefined();
            expect(validator.data).toBe(params);
        });
    });

    describe('validate', () => {
        it('Should succeed with valid params - ...', () => {
            // Arrange
            // generate an array of sourceIdItems
            const buildDataArray = new Array<number>();
            for (let itemIndex = 0; itemIndex < 191; itemIndex++) {
                // Add the sourceId Items buffer to the array
                buildDataArray.push(itemIndex);
            }

            const params: CrossPointTallyDumpByteCommandParams = {
                matrixId: 0,
                levelId: 0,
                numberOfTalliesReturned: buildDataArray.length,
                firstDestinationId: 0,
                sourceIdItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors).toBeDefined();
            expect(Object.keys(errors).length).toBe(0);
        });

        it('Should return errors id params are out of range < MIN', () => {
            // Arrange
            // generate an array of sourceIdItems
            const buildDataArray = new Array<number>();

            const params: CrossPointTallyDumpByteCommandParams = {
                matrixId: -1,
                levelId: -1,
                numberOfTalliesReturned: buildDataArray.length,
                firstDestinationId: -1,
                sourceIdItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.levelId).toBeDefined();
            expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.talliesReturned).toBeDefined();
            expect(errors.talliesReturned.id).toBe(
                CommandErrorsKeys.NUMBER_OF_TALLIES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.destinationId).toBeDefined();
            expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.sourceIdItems).toBeDefined();
            expect(errors.sourceIdItems.id).toBe(CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors if params are out of range > MAX', () => {
            // Arrange
            // generate an array of sourceIdItems
            const buildDataArray = new Array<number>();
            for (let itemIndex = 0; itemIndex < 192; itemIndex++) {
                // Add the sourceId Items buffer to the array
                buildDataArray.push(itemIndex);
            }
            const params: CrossPointTallyDumpByteCommandParams = {
                matrixId: 256,
                levelId: 256,
                numberOfTalliesReturned: buildDataArray.length,
                firstDestinationId: 256,
                sourceIdItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.levelId).toBeDefined();
            expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.talliesReturned).toBeDefined();
            expect(errors.talliesReturned.id).toBe(
                CommandErrorsKeys.NUMBER_OF_TALLIES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.destinationId).toBeDefined();
            expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.sourceIdItems).toBeDefined();
            expect(errors.sourceIdItems.id).toBe(CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors sourceIdItems params is empty', () => {
            // Arrange
            // generate an array of sourceIdItems
            const buildDataArray = new Array<number>();

            const params: CrossPointTallyDumpByteCommandParams = {
                matrixId: 0,
                levelId: 0,
                numberOfTalliesReturned: buildDataArray.length,
                firstDestinationId: 0,
                sourceIdItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.talliesReturned).toBeDefined();
            expect(errors.talliesReturned.id).toBe(
                CommandErrorsKeys.NUMBER_OF_TALLIES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.sourceIdItems).toBeDefined();
            expect(errors.sourceIdItems.id).toBe(CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG);
        });
    });
});
