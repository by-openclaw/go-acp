import { BootstrapService } from '../../../../src/bootstrap.service';
import { CrossPointTieLineTallyCommandParams } from '../../../../src/command/tx/113-crosspoint-tie-line-tally-message/params';
import { LocaleData } from '../../../../src/common/locale-data/locale-data.model';
import { CommandParamsValidator } from '../../../../src/command/tx/113-crosspoint-tie-line-tally-message/params.validator';
import { CrossPointTieLineTallyCommand } from '../../../../src/command/tx/113-crosspoint-tie-line-tally-message/command';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { CrossPointTieLineTallyCommandItems } from '../../../../src/command/tx/113-crosspoint-tie-line-tally-message/items';

describe('CrossPoint Tally Dump (Word) Message - CommandPAramsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            // generate an array of CrossPointTieLineTallyCommandItems
            const buildDataArray = new Array<CrossPointTieLineTallyCommandItems>();
            for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                // Add the CrossPoint Tie Line Tally Command Source Items buffer to the array
                // {sourceMatrixId: value, sourceLevel: value, sourceId: value}
                buildDataArray.push({ sourceMatrixId: 0, sourceLevel: 0, sourceId: itemIndex });
            }

            const params: CrossPointTieLineTallyCommandParams = {
                destinationMatrixId: 0,
                destinationAssociation: 0,
                numberOfSourcesReturned: buildDataArray.length,
                sourceItems: buildDataArray
            };

            // Act

            const validator = new CommandParamsValidator(params);
            const command = new CrossPointTieLineTallyCommand(params);
            command.buildCommand();

            // Assert
            expect(validator).toBeDefined();
            expect(validator.data).toBe(params);
        });
    });

    describe('validate', () => {
        it('Should succeed with valid params - ...', () => {
            // Arrange
            // generate an array of CrossPointTieLineTallyCommandItems
            const buildDataArray = new Array<CrossPointTieLineTallyCommandItems>();
            for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                // Add the CrossPoint Tie Line Tally Command Source Items buffer to the array
                // {sourceMatrixId: value, sourceLevel: value, sourceId: value}
                buildDataArray.push({ sourceMatrixId: 0, sourceLevel: 0, sourceId: itemIndex });
            }

            const params: CrossPointTieLineTallyCommandParams = {
                destinationMatrixId: 0,
                destinationAssociation: 0,
                numberOfSourcesReturned: buildDataArray.length,
                sourceItems: buildDataArray
            };

            // Act

            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();
            const command = new CrossPointTieLineTallyCommand(params);
            command.buildCommand();

            // Assert
            expect(errors).toBeDefined();
            expect(Object.keys(errors).length).toBe(0);
        });

        it('Should return errors id params are out of range < MIN', () => {
            // Arrange
            // generate an array of CrossPointTieLineTallyCommandItems
            const buildDataArray = new Array<CrossPointTieLineTallyCommandItems>();
            for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                // Add the CrossPoint Tie Line Tally Command Source Items buffer to the array
                // {sourceMatrixId: value, sourceLevel: value, sourceId: value}
                buildDataArray.push({ sourceMatrixId: -1, sourceLevel: -1, sourceId: -1 });
            }

            const params: CrossPointTieLineTallyCommandParams = {
                destinationMatrixId: -1,
                destinationAssociation: -1,
                numberOfSourcesReturned: -1,
                sourceItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.destinationMatrixId).toBeDefined();
            expect(errors.destinationMatrixId.id).toBe(
                CommandErrorsKeys.DESTINATION_MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.destinationAssociation).toBeDefined();
            expect(errors.destinationAssociation.id).toBe(
                CommandErrorsKeys.DESTINATION_ASSOCIATION_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.numberOfSourcesReturned).toBeDefined();
            expect(errors.numberOfSourcesReturned.id).toBe(
                CommandErrorsKeys.NUMBER_OF_SOURCES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.sourceItems).toBeDefined();
            expect(errors.sourceItems.id).toBe(CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors if params are out of range > MAX', () => {
            // Arrange
            // generate an array of CrossPointTieLineTallyCommandItems
            const buildDataArray = new Array<CrossPointTieLineTallyCommandItems>();
            for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                // Add the CrossPoint Tie Line Tally Command Source Items buffer to the array
                // {sourceMatrixId: value, sourceLevel: value, sourceId: value}
                buildDataArray.push({ sourceMatrixId: 256, sourceLevel: 256, sourceId: 65536 });
            }

            const params: CrossPointTieLineTallyCommandParams = {
                destinationMatrixId: 20,
                destinationAssociation: 65536,
                numberOfSourcesReturned: 257,
                sourceItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.destinationMatrixId).toBeDefined();
            expect(errors.destinationMatrixId.id).toBe(
                CommandErrorsKeys.DESTINATION_MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.destinationAssociation).toBeDefined();
            expect(errors.destinationAssociation.id).toBe(
                CommandErrorsKeys.DESTINATION_ASSOCIATION_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.numberOfSourcesReturned).toBeDefined();
            expect(errors.numberOfSourcesReturned.id).toBe(
                CommandErrorsKeys.NUMBER_OF_SOURCES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
            );
            expect(errors.sourceItems).toBeDefined();
            expect(errors.sourceItems.id).toBe(CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors sourceLevel params are out of range', () => {
            // Arrange
            // generate an array of CrossPointTieLineTallyCommandItems
            const buildDataArray = new Array<CrossPointTieLineTallyCommandItems>();
            for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                // Add the CrossPoint Tie Line Tally Command Source Items buffer to the array
                // {sourceMatrixId: value, sourceLevel: value, sourceId: value}
                buildDataArray.push({ sourceMatrixId: 0, sourceLevel: 256, sourceId: itemIndex });
            }

            const params: CrossPointTieLineTallyCommandParams = {
                destinationMatrixId: 0,
                destinationAssociation: 0,
                numberOfSourcesReturned: 4,
                sourceItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.sourceItems).toBeDefined();
            expect(errors.sourceItems.id).toBe(CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors sourceId params are out of range', () => {
            // Arrange
            // generate an array of CrossPointTieLineTallyCommandItems
            const buildDataArray = new Array<CrossPointTieLineTallyCommandItems>();
            for (let itemIndex = 0; itemIndex < 1; itemIndex++) {
                // Add the CrossPoint Tie Line Tally Command Source Items buffer to the array
                // {sourceMatrixId: value, sourceLevel: value, sourceId: value}
                buildDataArray.push({ sourceMatrixId: 0, sourceLevel: 0, sourceId: -1 });
            }

            const params: CrossPointTieLineTallyCommandParams = {
                destinationMatrixId: 0,
                destinationAssociation: 0,
                numberOfSourcesReturned: 1,
                sourceItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.sourceItems).toBeDefined();
            expect(errors.sourceItems.id).toBe(CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors sourceItems params is empty', () => {
            // Arrange
            // generate an array of CrossPointTieLineTallyCommandItems
            const buildDataArray = new Array<CrossPointTieLineTallyCommandItems>();

            const params: CrossPointTieLineTallyCommandParams = {
                destinationMatrixId: 0,
                destinationAssociation: 0,
                numberOfSourcesReturned: 0,
                sourceItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.sourceItems).toBeDefined();
            expect(errors.sourceItems.id).toBe(CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors sourceItems params are out of range', () => {
            // Arrange
            // generate an array of CrossPointTieLineTallyCommandItems
            const buildDataArray = new Array<CrossPointTieLineTallyCommandItems>();
            for (let itemIndex = 0; itemIndex < 257; itemIndex++) {
                // Add the CrossPoint Tie Line Tally Command Source Items buffer to the array
                // {sourceMatrixId: value, sourceLevel: value, sourceId: value}
                buildDataArray.push({ sourceMatrixId: 0, sourceLevel: 0, sourceId: itemIndex });
            }

            const params: CrossPointTieLineTallyCommandParams = {
                destinationMatrixId: 0,
                destinationAssociation: 0,
                numberOfSourcesReturned: 200,
                sourceItems: buildDataArray
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.sourceItems).toBeDefined();
            expect(errors.sourceItems.id).toBe(CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG);
        });
    });
});
